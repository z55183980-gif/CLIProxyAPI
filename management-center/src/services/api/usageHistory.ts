import { apiClient } from './client';

export interface UsagePrice {
  model: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cacheWrite1h?: number;
}
export interface UsageRecord {
  id: number;
  timestamp: string;
  requestId: string;
  sessionId: string;
  provider: string;
  model: string;
  alias: string;
  account: string;
  accountId: string;
  apiKeyId: string;
  endpoint: string;
  clientIp: string;
  userAgent: string;
  reasoningEffort: string;
  serviceTier: string;
  stream: boolean;
  failed: boolean;
  statusCode: number;
  error: string;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheWriteTokens: number;
  cacheWrite5mTokens?: number;
  cacheWrite1hTokens?: number;
  reasoningTokens: number;
  totalTokens: number;
  accountingQuality: string;
  latencyMs: number;
  ttftMs: number;
  cost: number | null;
  price: UsagePrice | null;
}
export interface UsageFilters {
  start: string;
  end: string;
  provider: string;
  model: string;
  accountId: string;
  apiKeyId: string;
  status: string;
  requestType: string;
  search: string;
  statusCode: string;
  page: number;
  pageSize: number;
  sort: string;
  order: string;
  snapshot?: number;
}
export interface UsageGroup {
  name: string;
  requests: number;
  tokens: number;
  cost: number;
}
export interface UsageResult {
  items: UsageRecord[];
  total: number;
  page: number;
  pageSize: number;
  snapshot: number;
  stats: {
    requests: number;
    failed: number;
    tokens: number;
    input: number;
    output: number;
    cacheRead: number;
    cacheWrite: number;
    cost: number;
    unpriced: number;
    latency: number;
    ttft: number;
  };
  models: UsageGroup[];
  accounts: UsageGroup[];
  trend: UsageGroup[];
  writeErrors: number;
}
export interface UsageOptions {
  providers: string[];
  models: string[];
  accounts: { id: string; name: string }[];
  apiKeys: string[];
}

export interface AccountUsageStats {
  requests: number;
  tokens: number;
  cost: number;
  unpriced: number;
  estimated?: number;
}
// The backend owns field naming; UI models consistently use camelCase.
function camelize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(camelize);
  if (value && typeof value === 'object')
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [
        key.replace(/_([a-z0-9])/g, (_, c: string) => c.toUpperCase()),
        camelize(item),
      ])
    );
  return value;
}
export function usageParams(f: UsageFilters) {
  return {
    start: f.start || undefined,
    end: f.end || undefined,
    provider: f.provider || undefined,
    model: f.model || undefined,
    account_id: f.accountId || undefined,
    api_key_id: f.apiKeyId || undefined,
    status: f.status || undefined,
    request_type: f.requestType || undefined,
    search: f.search || undefined,
    status_code: f.statusCode ? Number(f.statusCode) : undefined,
    page: f.page,
    page_size: f.pageSize,
    sort: f.sort,
    order: f.order,
    snapshot: f.snapshot,
  };
}
export const usageHistoryApi = {
  async accountTodayStats(accountIds: string[], start: string, end: string, signal: AbortSignal) {
    const response = await apiClient.post<{ stats: Record<string, AccountUsageStats> }>(
      '/usage-history/accounts/today',
      { account_ids: [...new Set(accountIds)], start, end },
      { signal }
    );
    return response.stats;
  },
  async list(filters: UsageFilters, signal: AbortSignal): Promise<UsageResult> {
    return camelize(
      await apiClient.get('/usage-history', { params: usageParams(filters), signal })
    ) as UsageResult;
  },
  async options(signal: AbortSignal): Promise<UsageOptions> {
    return camelize(await apiClient.get('/usage-history/options', { signal })) as UsageOptions;
  },
  async prices(signal: AbortSignal): Promise<UsagePrice[]> {
    const result = await apiClient.get<{ prices: unknown[] }>('/usage-prices', { signal });
    return camelize(result.prices) as UsagePrice[];
  },
  savePrices: (prices: UsagePrice[], signal: AbortSignal) =>
    apiClient.put(
      '/usage-prices',
      {
        prices: prices.map((p) => ({
          model: p.model,
          input: p.input,
          output: p.output,
          cache_read: p.cacheRead,
          cache_write: p.cacheWrite,
          cache_write_1h: p.cacheWrite1h,
        })),
      },
      { signal }
    ),
  cleanup: (filters: UsageFilters, signal: AbortSignal) =>
    apiClient.delete<{ deleted: number }>('/usage-history', {
      data: { filter: usageParams(filters), confirm: true },
      signal,
    }),
};
