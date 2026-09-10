import { expect, test } from 'bun:test';
import { usageParams, type UsageRecord } from '../src/services/api/usageHistory';
import { exportRow, initialUsageFilters, isValidUsageTimeRange } from '../src/features/usage/usageUtils';
import en from '../src/i18n/locales/en.json';
import zh from '../src/i18n/locales/zh-CN.json';

test('custom usage time requires a complete, strictly increasing range', () => {
  expect(isValidUsageTimeRange('2026-09-09T00:00', '2026-09-10T00:00')).toBe(true);
  expect(isValidUsageTimeRange('2026-09-11T00:02', '2026-09-10T00:00')).toBe(false);
  expect(isValidUsageTimeRange('2026-09-09T00:00', '2026-09-09T00:00')).toBe(false);
  expect(isValidUsageTimeRange('', '2026-09-10T00:00')).toBe(false);
  expect(isValidUsageTimeRange('2026-09-09T00:00', '')).toBe(false);
  expect(isValidUsageTimeRange('invalid', '2026-09-10T00:00')).toBe(false);
});

test('history filters preserve date ranges, paging, and account/key scope', () => {
  const filters={...initialUsageFilters(),accountId:'credential.json',apiKeyId:'key-hash',status:'failed',statusCode:'429',page:2,pageSize:50,snapshot:123};
  const params=usageParams(filters);
  expect(params.account_id).toBe('credential.json');expect(params.api_key_id).toBe('key-hash');
  expect(params.status_code).toBe(429);expect(params.snapshot).toBe(123);expect(params.page).toBe(2);
  expect(params.start).toBe(filters.start);expect(params.end).toBe(filters.end);
  expect(Date.parse(filters.end)).toBeGreaterThan(Date.parse(filters.start));
});
test('export keeps unknown costs distinct from free usage and treats text literally', () => {
  const row=exportRow({timestamp:'2026-09-09T00:00:00Z',account:'=SUM(A1)',apiKeyId:'hash',cost:null} as UsageRecord);
  expect(row.cost).toBe('');expect(row.account).toBe('=SUM(A1)');expect(row.apiKey).toBe('hash');
  expect(exportRow({cost:0} as UsageRecord).cost).toBe(0);
});
test('usage labels match in both supported languages', () => {
  expect(Object.keys(en.usageDetails).sort()).toEqual(Object.keys(zh.usageDetails).sort());
  for(const label of Object.values(zh.usageDetails))expect(label.length).toBeGreaterThan(0);
});
