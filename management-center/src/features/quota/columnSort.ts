import type { QuotaFileEntry } from './logic';

export type AccountSortField = 'priority' | 'concurrency' | 'weight';
export type AccountColumnSort = {
  field: AccountSortField;
  direction: 'ascending' | 'descending';
} | null;

export function nextAccountColumnSort(
  current: AccountColumnSort,
  field: AccountSortField
): AccountColumnSort {
  if (current?.field !== field) return { field, direction: 'ascending' };
  return current.direction === 'ascending' ? { field, direction: 'descending' } : null;
}

export function sortAccountColumns(
  entries: QuotaFileEntry[],
  sort: AccountColumnSort
): QuotaFileEntry[] {
  if (!sort) return [...entries];
  const value = ({ file }: QuotaFileEntry) => {
    const raw = file[sort.field];
    const fallback = sort.field === 'weight' ? 1 : 0;
    const number = typeof raw === 'number' && Number.isFinite(raw) ? raw : fallback;
    return sort.field === 'weight' ? Math.max(0, number) : number;
  };
  const direction = sort.direction === 'ascending' ? 1 : -1;
  return entries
    .map((entry, index) => ({ entry, index }))
    .sort((a, b) => direction * (value(a.entry) - value(b.entry)) || a.index - b.index)
    .map(({ entry }) => entry);
}
