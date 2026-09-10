import { describe, expect, test } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { createInstance } from 'i18next';
import { I18nextProvider } from 'react-i18next';
import {
  nextAccountColumnSort,
  sortAccountColumns,
  type AccountSortField,
} from '../src/features/quota/columnSort';
import { AccountSortHeader } from '../src/features/quota/components/AccountSortHeader';
import { paginate, type QuotaFileEntry } from '../src/features/quota/logic';
import zhCN from '../src/i18n/locales/zh-CN.json';

const entries: QuotaFileEntry[] = [
  { type: 'codex', file: { name: 'a', priority: 10, concurrency: 10, weight: 10 } },
  { type: 'codex', file: { name: 'b', priority: 2, concurrency: 2, weight: 2 } },
  { type: 'claude', file: { name: 'c', priority: 2, concurrency: 2, weight: 2 } },
];
const names = (items: QuotaFileEntry[]) => items.map(({ file }) => file.name);

describe('account column sorting', () => {
  for (const field of ['priority', 'concurrency', 'weight'] as AccountSortField[]) {
    test(`${field} cycles ascending, descending, restored with stable ties`, () => {
      const ascending = nextAccountColumnSort(null, field);
      expect(ascending).toEqual({ field, direction: 'ascending' });
      expect(names(sortAccountColumns(entries, ascending))).toEqual(['b', 'c', 'a']);
      const descending = nextAccountColumnSort(ascending, field);
      expect(names(sortAccountColumns(entries, descending))).toEqual(['a', 'b', 'c']);
      const restored = nextAccountColumnSort(descending, field);
      expect(restored).toBeNull();
      expect(names(sortAccountColumns(entries, restored))).toEqual(['a', 'b', 'c']);
      expect(names(entries)).toEqual(['a', 'b', 'c']);
      expect(names(paginate(sortAccountColumns(entries, ascending), 1, 1).pageItems)).toEqual([
        'b',
      ]);
    });
  }
  test('changing columns starts in ascending order', () => {
    expect(nextAccountColumnSort({ field: 'priority', direction: 'descending' }, 'weight')).toEqual(
      { field: 'weight', direction: 'ascending' }
    );
  });
  test('missing values match editor defaults and zero weight stays excluded', () => {
    const files: QuotaFileEntry[] = [
      { type: 'codex', file: { name: 'default' } },
      { type: 'codex', file: { name: 'zero', weight: 0, priority: -1, concurrency: 5 } },
      { type: 'codex', file: { name: 'negative', weight: -2, priority: 1, concurrency: 1 } },
    ];
    expect(names(sortAccountColumns(files, { field: 'weight', direction: 'ascending' }))).toEqual([
      'zero',
      'negative',
      'default',
    ]);
    expect(
      names(sortAccountColumns(files, { field: 'concurrency', direction: 'ascending' }))
    ).toEqual(['default', 'negative', 'zero']);
    expect(names(sortAccountColumns(files, { field: 'priority', direction: 'ascending' }))).toEqual(
      ['zero', 'default', 'negative']
    );
  });
  test('headers expose current direction and next action to keyboard and screen readers', async () => {
    const i18n = createInstance();
    await i18n.init({ lng: 'zh-CN', resources: { 'zh-CN': { translation: zhCN } } });
    for (const direction of ['none', 'ascending', 'descending'] as const) {
      const markup = renderToStaticMarkup(
        createElement(
          I18nextProvider,
          { i18n },
          createElement(
            'table',
            null,
            createElement(
              'thead',
              null,
              createElement(
                'tr',
                null,
                createElement(AccountSortHeader, {
                  field: 'priority',
                  label: '优先级',
                  className: '',
                  sort: direction === 'none' ? null : { field: 'priority', direction },
                  onSort: () => {},
                })
              )
            )
          )
        )
      );
      expect(markup).toContain(`aria-sort="${direction}"`);
      expect(markup).toContain('<button type="button"');
      expect(markup).toContain(
        direction === 'none' ? '升序' : direction === 'ascending' ? '降序' : '还原'
      );
    }
  });
});
