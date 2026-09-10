import { expect, spyOn, test } from 'bun:test';
import { apiClient } from '../src/services/api/client';
import { usageHistoryApi } from '../src/services/api/usageHistory';
import { exportRow, initialUsageFilters } from '../src/features/usage/usageUtils';

test('cache TTL buckets and prices survive API normalization and export', async () => {
  const get = spyOn(apiClient, 'get').mockResolvedValue({
    items: [{ cache_write_tokens: 100, cache_write_5m_tokens: 30, cache_write_1h_tokens: 70,
      price: { cache_write: 12.5, cache_write_1h: 20 } }],
  });
  try {
    const result = await usageHistoryApi.list(initialUsageFilters(), new AbortController().signal);
    const record = result.items[0];
    expect(record.cacheWrite5mTokens).toBe(30);
    expect(record.cacheWrite1hTokens).toBe(70);
    expect(record.price?.cacheWrite1h).toBe(20);
    expect(exportRow(record).cacheWrite1h).toBe(70);
    expect(exportRow(record).cacheWrite).toBe(100);
  } finally { get.mockRestore(); }
});

test('custom 1h price preserves zero and omitted legacy pricing', async () => {
  const put = spyOn(apiClient, 'put').mockResolvedValue({});
  try {
    await usageHistoryApi.savePrices([
      { model: 'explicit-free', input: 1, output: 2, cacheRead: 0.1, cacheWrite: 3, cacheWrite1h: 0 },
      { model: 'legacy', input: 1, output: 2, cacheRead: 0.1, cacheWrite: 3 },
    ], new AbortController().signal);
    const body = put.mock.calls[0][1] as { prices: { cache_write_1h?: number }[] };
    expect(body.prices[0].cache_write_1h).toBe(0);
    expect(body.prices[1].cache_write_1h).toBeUndefined();
  } finally { put.mockRestore(); }
});
