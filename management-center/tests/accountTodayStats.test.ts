import { describe, expect, test, spyOn } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import { AccountTodayStatsCell } from '../src/features/quota/components/AccountTodayStatsCell';
import {
  todayStatsAccountId,
  todayStatsFilters,
  todayStatsFromUsage,
  formatTodayTokens,
} from '../src/features/quota/todayStats';
import type { UsageResult } from '../src/services/api/usageHistory';
import { usageHistoryApi } from '../src/services/api/usageHistory';
import { apiClient } from '../src/services/api/client';
import zhCN from '../src/i18n/locales/zh-CN.json';

const i18n = createInstance();
await i18n.init({ lng: 'zh-CN', resources: { 'zh-CN': { translation: zhCN } } });
const stats: UsageResult['stats'] = {
  requests: 2430,
  tokens: 298510000,
  cost: 405.39,
  unpriced: 0,
  failed: 0,
  input: 0,
  output: 0,
  cacheRead: 0,
  cacheWrite: 0,
  latency: 0,
  ttft: 0,
};
const render = (value?: Parameters<typeof AccountTodayStatsCell>[0]['stats']) =>
  renderToStaticMarkup(
    createElement(I18nextProvider, { i18n }, createElement(AccountTodayStatsCell, { stats: value }))
  );

describe('account daily statistics', () => {
  test('matches the reference number and currency format', () => {
    const markup = render({
      requests: 2430,
      tokens: 298510000,
      accountCost: 405.39,
      userCost: null,
    });
    for (const value of ['2,430', '298.51M', 'US$405.39', '请求:', 'Token:', '账号计费:'])
      expect(markup).toContain(value);
  });
  test('keeps three placeholders for absent data and preserves actual zeros', () => {
    expect(render().match(/>--</g)).toHaveLength(3);
    const zero = render({ requests: 0, tokens: 0, accountCost: 0, userCost: null });
    expect(zero).toContain('US$0.00');
    expect(zero).not.toContain('用户扣费');
  });
  test('uses the history account identity without reading credential secrets', () => {
    expect(
      todayStatsAccountId({
        name: 'same-name',
        id: 'auth-id',
        authIndex: 'index',
        account: 'secret',
      })
    ).toBe('auth-id');
    expect(todayStatsAccountId({ name: 'same-name', authIndex: 12 })).toBe('12');
    expect(todayStatsAccountId({ name: 'same-name', account: 'secret' })).toBeNull();
  });
  test('queries the entire local calendar day without filtering failed requests or truncating totals', () => {
    const filter = todayStatsFilters('auth-id', '2026-09-09');
    expect(new Date(filter.start).getHours()).toBe(0);
    expect(new Date(filter.end).getDate()).toBe(10);
    expect(filter.accountId).toBe('auth-id');
    expect(filter.status).toBe('');
    expect(filter.pageSize).toBe(1);
    expect(todayStatsFromUsage(stats).requests).toBe(2430);
  });
  test('does not invent user charges or show partial model costs as complete', () => {
    expect(todayStatsFromUsage(stats)).toEqual({
      requests: 2430,
      tokens: 298510000,
      accountCost: 405.39,
      userCost: null,
      estimated: 0,
      unpriced: 0,
    });
    expect(todayStatsFromUsage({ ...stats, unpriced: 1 }).accountCost).toBeNull();
    expect(todayStatsFromUsage({ ...stats, cost: NaN }).accountCost).toBeNull();
    expect(formatTodayTokens(12500)).toBe('12.5K');
  });
  test('fetches all visible accounts in one batch without changing account IDs', async () => {
    const post = spyOn(apiClient, 'post').mockResolvedValue({
      stats: {
        account_with_underscore: {
          requests: 1,
          tokens: 250,
          cost: 0.001,
          unpriced: 0,
          estimated: 1,
        },
      },
    });
    try {
      const signal = new AbortController().signal;
      const result = await usageHistoryApi.accountTodayStats(
        ['account_with_underscore', 'account_with_underscore'],
        'start',
        'end',
        signal
      );
      expect(post).toHaveBeenCalledWith(
        '/usage-history/accounts/today',
        {
          account_ids: ['account_with_underscore'],
          start: 'start',
          end: 'end',
        },
        { signal }
      );
      const converted = todayStatsFromUsage(result.account_with_underscore);
      expect(converted.accountCost).toBe(0.001);
      expect(converted.estimated).toBe(1);
      expect(render(converted)).toContain('按当前模型价格估算');
      expect(
        render(todayStatsFromUsage({ ...result.account_with_underscore, unpriced: 1 }))
      ).toContain('缺少模型价格或完整 Token 用量');
    } finally {
      post.mockRestore();
    }
  });
});
