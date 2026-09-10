import type { UsageFilters, UsageRecord } from '@/services/api/usageHistory';

export function initialUsageFilters(): UsageFilters {
  const start = new Date();
  start.setHours(0, 0, 0, 0);
  const end = new Date(start);
  end.setDate(end.getDate() + 1);
  return {
    start: start.toISOString(),
    end: end.toISOString(),
    provider: '',
    model: '',
    accountId: '',
    apiKeyId: '',
    status: '',
    requestType: '',
    search: '',
    statusCode: '',
    page: 1,
    pageSize: 25,
    sort: 'timestamp',
    order: 'desc',
  };
}
export function localDateTime(value: string): string {
  if (!value) return '';
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}
export function isValidUsageTimeRange(start: string, end: string): boolean {
  const startTime = Date.parse(start);
  const endTime = Date.parse(end);
  return Number.isFinite(startTime) && Number.isFinite(endTime) && endTime > startTime;
}
export function exportRow(record: UsageRecord): Record<string, string | number> {
  return {
    time: record.timestamp,
    account: record.account,
    apiKey: record.apiKeyId,
    provider: record.provider,
    model: record.model,
    requestedModel: record.alias,
    requestId: record.requestId,
    sessionId: record.sessionId,
    endpoint: record.endpoint,
    type: record.stream ? 'stream' : 'non_stream',
    status: record.statusCode,
    input: record.inputTokens,
    output: record.outputTokens,
    cacheRead: record.cacheReadTokens,
    cacheWrite: record.cacheWriteTokens,
    cacheWrite5m: record.cacheWrite5mTokens ?? '',
    cacheWrite1h: record.cacheWrite1hTokens ?? '',
    reasoning: record.reasoningTokens,
    total: record.totalTokens,
    cost: record.cost ?? '',
    latency: record.latencyMs,
    ttft: record.ttftMs,
    ip: record.clientIp,
    userAgent: record.userAgent,
  };
}
