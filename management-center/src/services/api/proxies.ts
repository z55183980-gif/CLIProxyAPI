import { apiClient } from './client';
import type { AuthFilesResponse } from '@/types/authFile';

export interface ProxyAccount {
  id: string;
  name: string;
  protocol: string;
  host: string;
  port: number;
  username: string;
  password?: string;
  status: string;
  expiresAt: string;
  fallbackMode: string;
  backupProxyId: string;
  expiryWarnDays: number;
  latencyMs: number | null;
  latencyStatus: string;
  location: string;
  accountCount: number;
}
type Raw = Record<string, unknown>;
export function normalizeProxy(p: Raw): ProxyAccount {
  return {
    id: String(p.id),
    name: String(p.name || ''),
    protocol: String(p.protocol || 'http'),
    host: String(p.host || ''),
    port: Number(p.port),
    username: String(p.username || ''),
    status: String(p.status || 'active'),
    expiresAt: String(p.expires_at || ''),
    fallbackMode: String(p.fallback_mode || 'none'),
    backupProxyId: String(p.backup_proxy_id || ''),
    expiryWarnDays: Number(p.expiry_warn_days ?? 7),
    latencyMs: p.latency_ms == null ? null : Number(p.latency_ms),
    latencyStatus: String(p.latency_status || ''),
    location: [
      ...new Set([p.observed_country, p.observed_region, p.observed_city].filter(Boolean)),
    ].join(' · '),
    accountCount: Number(p.account_count || 0),
  };
}
export function serializeProxy(p: ProxyAccount) {
  return {
    name: p.name,
    protocol: p.protocol,
    host: p.host,
    port: p.port,
    username: p.username,
    password: p.password || '',
    status: p.status,
    expires_at: p.expiresAt || null,
    fallback_mode: p.fallbackMode,
    backup_proxy_id: p.backupProxyId,
    expiry_warn_days: p.expiryWarnDays,
  };
}
interface BatchResult {
  data: { success_ids: string[]; failures: { message: string }[] };
}
const checkBatch = (result: BatchResult) => {
  if (result.data.failures.length)
    throw new Error(result.data.failures.map((f) => f.message).join('; '));
};
const path = (id: string) => `/proxy-accounts/${encodeURIComponent(id)}`;
export const proxiesApi = {
  async list(signal: AbortSignal) {
    const result = await apiClient.get<{ proxies: Raw[] }>('/proxy-accounts', { signal });
    return (result.proxies || []).map(normalizeProxy);
  },
  save: (p: ProxyAccount) =>
    p.id
      ? apiClient.put(path(p.id), serializeProxy(p))
      : apiClient.post('/proxy-accounts', serializeProxy(p)),
  remove: (id: string) => apiClient.delete(path(id)),
  async test(id: string) {
    const result = await apiClient.post<{
      success: boolean;
      error?: string;
      result?: { error?: string };
    }>(`${path(id)}/test`);
    if (!result.success)
      throw new Error(result.error || result.result?.error || 'Connection test failed');
  },
  async boundAccounts(id: string) {
    const result = await apiClient.get<AuthFilesResponse>('/auth-files');
    return result.files.filter((f) => f.proxy_id === id).map((f) => f.email || f.name);
  },
  async import(items: unknown[]) {
    checkBatch(await apiClient.post<BatchResult>('/proxy-accounts/batch', { items }));
  },
  async batch(ids: string[], status?: string) {
    const result = status
      ? await apiClient.put<BatchResult>('/proxy-accounts/batch', { ids, patch: { status } })
      : await apiClient.delete<BatchResult>('/proxy-accounts/batch', { data: { ids } });
    checkBatch(result);
  },
};
