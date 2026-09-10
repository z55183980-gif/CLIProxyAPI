import { describe, expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import type { CodexQuotaState } from '../src/types';
import { usageWindowsFor, usageResetText } from '../src/features/quota/usageWindows';
import { ChannelWindowRows } from '../src/features/quota/components/ChannelUsageWindowCell';
import { UsageWindowCell } from '../src/features/quota/components/UsageWindowCell';
import zhCN from '../src/i18n/locales/zh-CN.json';

const i18n = createInstance();
await i18n.init({ lng: 'zh-CN', resources: { 'zh-CN': { translation: zhCN } } });
const noop = () => {};
const render = (quota?: CodexQuotaState, canReset = false) =>
  renderToStaticMarkup(
    createElement(
      I18nextProvider,
      { i18n },
      createElement(UsageWindowCell, {
        type: 'codex',
        quota,
        canRefresh: true,
        canReset,
        resetting: false,
        onRefresh: noop,
        onReset: noop,
      })
    )
  );
const makeQuota = (): CodexQuotaState => ({
  status: 'success',
  windows: [
    { id: 'five-hour', label: 'Five hour', usedPercent: 55, resetLabel: '', periodHours: 5 },
    { id: 'weekly', label: 'Weekly', usedPercent: 24, resetLabel: '', periodHours: 168 },
  ],
});

describe('compact usage windows', () => {
  test('does not fabricate windows or statistics before loading', () => {
    for (const quota of [
      undefined,
      { status: 'loading' as const, windows: [] },
      { status: 'error' as const, windows: [], error: 'failed' },
    ]) {
      const markup = render(quota);
      expect(markup).not.toContain('role="progressbar"');
      expect(markup).not.toContain('U $');
      expect(markup).not.toContain('-- req');
      for (const label of ['查询', '次数', '重置']) expect(markup).toContain(label);
    }
  });
  test.each(['claude', 'codex'])(
    '%s renders separate 5h and 7d statistics without user costs',
    (type) => {
      const markup = renderToStaticMarkup(
        createElement(
          I18nextProvider,
          { i18n },
          createElement(ChannelWindowRows, {
            type,
            now: 1700000000000,
            data: {
              source: 'passive',
              windows: [
                {
                  id: 'five-hour',
                  label: '5h',
                  usedPercent: 55,
                  stats: { requests: 10, tokens: 1200, cost: 2, unpriced: 0 },
                },
                {
                  id: type === 'claude' ? 'seven-day' : 'weekly',
                  label: '7d',
                  usedPercent: 24,
                  stats: { requests: 20, tokens: 2200, cost: 4, unpriced: 0 },
                },
                {
                  id: 'empty',
                  label: 'Empty',
                  usedPercent: 0,
                  stats: { requests: 0, tokens: 0, cost: 0, unpriced: 0 },
                },
              ],
            },
          })
        )
      );
      expect(markup).toContain('aria-valuenow="55"');
      expect(markup).toContain('10 req');
      expect(markup).toContain('20 req');
      expect(markup).toContain('1.2K');
      expect(markup).toContain('A $2.00');
      expect(markup).toContain('A $4.00');
      expect(markup.match(/ req<\/span>/g)).toHaveLength(2);
      expect(markup).not.toContain('U $');
    }
  );
  test('Claude exposes query without Codex credit actions', () => {
    const markup = renderToStaticMarkup(
      createElement(
        I18nextProvider,
        { i18n },
        createElement(UsageWindowCell, {
          type: 'claude',
          canRefresh: true,
          canReset: false,
          resetting: false,
          onRefresh: noop,
          onReset: noop,
        })
      )
    );
    expect(markup).toContain('查询');
    expect(markup).not.toContain('次数');
    expect(markup).not.toContain('重置');
    expect(markup).not.toContain('U $');
  });
  test('keeps review and monthly data distinct from primary windows', () => {
    const quota = makeQuota();
    quota.windows = [
      { ...quota.windows[0], id: 'code-review-five-hour' },
      { ...quota.windows[1], id: 'monthly', periodHours: 720 },
    ];
    const windows = usageWindowsFor('codex', quota);
    expect(windows.slice(0, 2).map((window) => window.usedPercent)).toEqual([null, null]);
    expect(windows.slice(2).map((window) => window.id)).toEqual([
      'code-review-five-hour',
      'monthly',
    ]);
  });
  test('formats reset countdown without making unknown data appear available', () => {
    const now = 1700000000000;
    const labels = { now: '现在', pending: '待刷新' };
    const window = {
      id: 'weekly',
      label: '7d',
      usedPercent: 24,
      resetAtMs: now + (5 * 24 + 14) * 3600000,
    };
    expect(usageResetText(window, now, labels)).toBe('5d 14h');
    expect(usageResetText({ ...window, usedPercent: 0 }, now, labels)).toBe('现在');
    expect(usageResetText({ ...window, usedPercent: 0 }, now, labels, false)).toBe('5d 14h');
    expect(usageResetText({ ...window, resetAtMs: now - 1 }, now, labels)).toBe('待刷新');
    expect(usageResetText({ ...window, usedPercent: null, resetAtMs: null }, now, labels)).toBe(
      '--'
    );
  });
  test('preserves zero credits and keeps reset disabled without credits', () => {
    const quota = { ...makeQuota(), rateLimitResetCreditsAvailableCount: 0 };
    const markup = render(quota);
    expect(markup).toContain('次数 0');
    const reset = markup.match(
      new RegExp(`<button[^>]*title="${zhCN.codex_quota.reset_button}"[^>]*>`)
    )?.[0];
    expect(reset).toContain('disabled=""');
    const enabled = render({ ...quota, rateLimitResetCreditsAvailableCount: 2 }, true);
    expect(
      enabled.match(new RegExp(`<button[^>]*title="${zhCN.codex_quota.reset_button}"[^>]*>`))?.[0]
    ).not.toContain('disabled');
  });
});
