import type { AuthFileItem } from '@/types';
import type { UsageFilters, AccountUsageStats } from '@/services/api/usageHistory';
import { normalizeAuthIndex } from '@/utils/authIndex';

export interface AccountTodayStats {
  requests: number | null;
  tokens: number | null;
  accountCost: number | null;
  userCost: number | null;
  estimated?: number;
  unpriced?: number;
}

// Usage history records AuthID first and falls back to AuthIndex, never the display name.
export function todayStatsAccountId(file: AuthFileItem): string | null {
  return normalizeAuthIndex(file.id) ?? normalizeAuthIndex(file.authIndex);
}

export function todayStatsFilters(accountId: string, day: string): UsageFilters {
  const start = new Date(`${day}T00:00:00`);
  const end = new Date(start);
  end.setDate(end.getDate() + 1);
  return {
    start: start.toISOString(),
    end: end.toISOString(),
    accountId,
    provider: '',
    model: '',
    apiKeyId: '',
    status: '',
    requestType: '',
    search: '',
    statusCode: '',
    page: 1,
    pageSize: 1,
    sort: 'timestamp',
    order: 'desc',
  };
}

const valid = (value: unknown): number | null =>
  typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null;
export function todayStatsFromUsage(stats: AccountUsageStats): AccountTodayStats {
  return {
    requests: valid(stats.requests),
    tokens: valid(stats.tokens),
    // A partial sum is not a complete daily cost; unknown prices remain unknown.
    accountCost: stats.unpriced === 0 ? valid(stats.cost) : null,
    userCost: null,
    estimated: stats.estimated ?? 0,
    unpriced: stats.unpriced,
  };
}

export function formatTodayTokens(value: number | null): string {
  if (value === null) return '--';
  if (value >= 1000000) return `${(value / 1000000).toFixed(2)}M`;
  if (value >= 1000) return `${(value / 1000).toFixed(1)}K`;
  return String(value);
}
