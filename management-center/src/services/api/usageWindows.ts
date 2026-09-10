import { apiClient } from './client';
import type { AccountUsageStats } from './usageHistory';

export interface AccountUsageWindow {
  id: string;
  label: string;
  usedPercent: number;
  resetAt?: string;
  stats?: AccountUsageStats;
}

export interface AccountUsageWindows {
  windows: AccountUsageWindow[];
  source: 'active' | 'passive';
  error?: string;
  statsError?: string;
}

export const accountUsageWindowsApi = {
  get(authIndex: string, active: boolean, signal: AbortSignal) {
    return apiClient.get<AccountUsageWindows>('/auth-files/usage-windows', {
      params: { auth_index: authIndex, source: active ? 'active' : 'passive' },
      signal,
      timeout: 0,
    });
  },
};
