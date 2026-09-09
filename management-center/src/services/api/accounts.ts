import { apiClient } from './client';
import type { AuthFilesResponse } from '@/types/authFile';

type Raw = Record<string, unknown>;
const object = (value: unknown): Raw => (value && typeof value === 'object' ? (value as Raw) : {});
const text = (value: unknown) => (typeof value === 'string' ? value : '');
const number = (value: unknown): number | null => {
  if (value === null || value === undefined || value === '') return null;
  const result = Number(value);
  return Number.isFinite(result) ? result : null;
};
export interface AccountSummary {
  id: string;
  name: string;
  status: string;
  message: string;
  fiveHour: number | null;
  sevenDay: number | null;
  rpm: number | null;
  tpm: number | null;
  concurrency: number | null;
  requests: number | null;
  tokens: number | null;
  cost: number | null;
}

export function normalizeAccounts(
  files: AuthFilesResponse['files'],
  totals: Raw[]
): AccountSummary[] {
  return files
    .filter((file) => /claude|anthropic/i.test([file.provider, file.type, file.name].join(' ')))
    .map((file) => {
      const identities = [
        file.id,
        file.auth_id,
        file.auth_index,
        file.email,
        file.name,
        file.account,
      ];
      const total = totals.find(
        (row) => row.account !== 'total' && identities.includes(row.account)
      );
      const quota = object(file.quota);
      const signals = object(quota.signals);
      const utilization = (keys: string[]) => {
        const key = Object.keys(signals).find((key) => keys.includes(key.toLowerCase()));
        const value = number(key ? signals[key] : null);
        return value === null ? null : value <= 1 ? value * 100 : value;
      };
      return {
        id: text(file.id) || file.name,
        // The raw account field can contain an API key. Use it only for matching.
        name: text(file.email) || file.name,
        status: file.disabled ? 'inactive' : text(file.status) || 'active',
        message: text(file.status_message),
        fiveHour: utilization([
          'anthropic-ratelimit-unified-5h-utilization',
          '5h_utilization',
          '5h',
        ]),
        sevenDay: utilization([
          'anthropic-ratelimit-unified-7d-utilization',
          '7d_utilization',
          '7d',
        ]),
        rpm: number(quota.rpm ?? quota.rpm_limit),
        tpm: number(quota.tpm ?? quota.tpm_limit),
        concurrency: number(quota.concurrency ?? quota.concurrent_limit),
        requests: number(total?.requests),
        tokens: number(total?.total_tokens),
        cost: total ? (number(total.total_micros) ?? 0) / 1e6 : null,
      };
    });
}

export const accountsApi = {
  async list(signal: AbortSignal) {
    const [files, totals] = await Promise.all([
      apiClient.get<AuthFilesResponse>('/auth-files', { signal }),
      apiClient.get<{ accounts: Raw[] }>('/billing-usage', { signal }),
    ]);
    return normalizeAccounts(files.files || [], totals.accounts || []);
  },
};
