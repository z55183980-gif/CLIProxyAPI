import { describe, expect, mock, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import type { QuotaRowProps } from '../src/features/quota/components/QuotaRow';
import zhCN from '../src/i18n/locales/zh-CN.json';

// Bun does not compile SCSS modules; use the class names from the real stylesheet.
const quotaStyles = readFileSync(
  new URL('../src/features/quota/components/QuotaBody.module.scss', import.meta.url),
  'utf8'
);
mock.module('../src/features/quota/components/QuotaBody.module.scss', () => ({
  default: Object.fromEntries(
    [...quotaStyles.matchAll(/\.([a-zA-Z][\w-]*)/g)].map((match) => [match[1], match[1]])
  ),
}));
const { QuotaRow } = await import('../src/features/quota/components/QuotaRow');
const i18n = createInstance();
await i18n.init({ lng: 'zh-CN', resources: { 'zh-CN': { translation: zhCN } } });
const noop = () => {};
function renderRow(overrides: Partial<QuotaRowProps> = {}) {
  return renderToStaticMarkup(
    createElement(
      I18nextProvider,
      { i18n },
      createElement(
        'table',
        null,
        createElement(
          'tbody',
          null,
          createElement(QuotaRow, {
            entry: {
              type: 'codex',
              file: { name: 'test.json', email: 'test@example.com', account: 'secret-value' },
            },
            resolvedTheme: 'light',
            canRefresh: true,
            resetting: false,
            onRefresh: noop,
            onReset: noop,
            ...overrides,
          })
        )
      )
    )
  );
}

describe('account management quota list', () => {
  test('the account management route renders the quota table', () => {
    expect(zhCN.nav.quota_management).toBe('账号管理');
    const routes = readFileSync(new URL('../src/router/MainRoutes.tsx', import.meta.url), 'utf8');
    expect(routes).toContain("{ path: '/quota', element: <QuotaPage /> }");
    const page = readFileSync(
      new URL('../src/features/quota/QuotaPage.tsx', import.meta.url),
      'utf8'
    );
    expect(page).toContain('<table');
    expect(page).toContain('<QuotaRow');
    expect(page).not.toContain('<QuotaCard');
  });
  test('renders eight cells with account identity and retains click-to-load', () => {
    const markup = renderRow();
    expect(markup.match(/<td(?:\s|>)/g)).toHaveLength(8);
    expect(markup).toContain('test@example.com');
    expect(markup).not.toContain('secret-value');
    expect(markup).not.toContain('U $');
    expect(markup).toContain(zhCN.quota_management.window_query);
  });
  test('blocks refresh while disconnected, loading, or resetting', () => {
    for (const props of [
      { canRefresh: false },
      { quota: { status: 'loading' as const } },
      { resetting: true },
    ]) {
      const markup = renderRow(props);
      const refresh = markup.match(
        new RegExp(`<button[^>]*title="${zhCN.codex_quota.reset_credits_label}"[^>]*>`)
      )?.[0];
      expect(refresh).toContain('disabled=""');
    }
  });
  test('preserves quota error details within the list row', () => {
    const markup = renderRow({ quota: { status: 'error', error: 'upstream unavailable' } });
    expect(markup).toContain('role="alert"');
    expect(markup).toContain('upstream unavailable');
    expect(markup.match(/<td(?:\s|>)/g)).toHaveLength(8);
  });
});

test('capacity, priority and weight are inline fields without the old details operation', () => {
  const markup = renderRow({
    canEdit: true,
    entry: {
      type: 'codex',
      file: {
        name: 'account.json',
        currentConcurrency: 2,
        concurrency: 5,
        priority: 1,
        weight: 10,
        capacityEditable: true,
      },
    },
  });
  expect(markup).toContain('2 /');
  expect(markup).toContain('value="5"');
  expect(markup).toContain('value="1"');
  expect(markup).toContain('value="10"');
  expect(markup.indexOf('的优先级')).toBeLessThan(markup.indexOf('的调度权重'));
  expect(markup).not.toContain('<details');
  expect(markup.match(/type="number"/g)).toHaveLength(3);
});

test('weight defaults to one and preserves exclusion with zero', () => {
  for (const weight of [undefined, 0, 10]) {
    const markup = renderRow({
      canEdit: true,
      entry: { type: 'codex', file: { name: 'weight.json', capacityEditable: true, weight } },
    });
    const input = markup.match(/<input[^>]*aria-label="[^"]*的调度权重"[^>]*>/)?.[0];
    expect(input).toContain(`value="${weight ?? 1}"`);
    expect(input).toContain('max="1000000"');
    expect(input).not.toContain('disabled');
  }
  const markup = renderRow({ canEdit: false });
  expect(markup.match(/<input[^>]*的调度权重[^>]*>/)?.[0]).toContain('disabled');
});
