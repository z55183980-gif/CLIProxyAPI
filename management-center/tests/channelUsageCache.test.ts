import { expect, test } from 'bun:test';
import { ChannelUsageCache } from '../src/features/quota/channelUsageCache';
import type { AccountUsageWindows } from '../src/services/api/usageWindows';

test('usage cache expires deterministically and isolates accounts, revisions and connections', () => {
  const cache = new ChannelUsageCache();
  const data: AccountUsageWindows = { source: 'passive', windows: [] };
  cache.set(1, 'claude:a', 'first', data, 1000);
  expect(cache.get(1, 'claude:a', 'first', 1001)).toBe(data);
  expect(cache.get(1, 'codex:a', 'first', 1001)).toBeNull();
  expect(cache.get(1, 'claude:a', 'refreshed', 1001)).toBeNull();
  expect(cache.get(1, 'claude:a', 'first', 60999)).toBe(data);
  expect(cache.get(1, 'claude:a', 'first', 61000)).toBeNull();
  expect(cache.get(2, 'claude:a', 'first', 1001)).toBeNull();
  expect(cache.get(1, 'claude:a', 'first', 1001)).toBeNull();
});
