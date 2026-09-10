import type {
  AntigravityQuotaState,
  ClaudeQuotaState,
  CodexQuotaState,
  KimiQuotaState,
  XaiQuotaState,
} from '@/types';
import type { QuotaCardState } from './providers';
import type { QuotaProviderType } from './providers/types';

export type UsageWindow = {
  id: string;
  label: string;
  usedPercent: number | null;
  resetAtMs?: number | null;
  periodHours?: number | null;
};

const finite = (value: number | null | undefined): number | null =>
  typeof value === 'number' && Number.isFinite(value) ? value : null;

export function usageWindowsFor(type: QuotaProviderType, quota?: QuotaCardState): UsageWindow[] {
  let windows: UsageWindow[] = [];
  if (quota?.status === 'success') {
    if (type === 'codex' || type === 'claude') {
      windows = ((quota as CodexQuotaState | ClaudeQuotaState).windows ?? []).map((window) => ({
        ...window,
      }));
    } else if (type === 'kimi') {
      windows = ((quota as KimiQuotaState).rows ?? []).map((row) => ({
        ...row,
        label: row.label ?? row.id,
        usedPercent: row.limit > 0 ? (row.used / row.limit) * 100 : null,
      }));
    } else if (type === 'antigravity') {
      windows = ((quota as AntigravityQuotaState).groups ?? []).flatMap((group) =>
        group.buckets.map((bucket) => ({
          ...bucket,
          id: `${group.id}:${bucket.id}`,
          usedPercent: (1 - bucket.remainingFraction) * 100,
        }))
      );
    } else {
      const billing = (quota as XaiQuotaState).billing;
      if (billing?.mode === 'billing') {
        windows = [
          {
            id: 'billing',
            label: billing.periodType,
            usedPercent: billing.usagePercent,
            resetAtMs: billing.resetAtMs,
            periodHours: billing.periodHours,
          },
        ];
      }
    }
  }
  windows = windows.map((window) => ({ ...window, usedPercent: finite(window.usedPercent) }));
  // Match only the main windows. Review/model-specific windows must not impersonate them.
  const primary = windows.find((window) => ['five-hour', 'five_hour'].includes(window.id));
  const weekly = windows.find((window) => ['weekly', 'seven-day', 'seven_day'].includes(window.id));
  const placeholder = (id: string, label: string): UsageWindow => ({
    id,
    label,
    usedPercent: null,
  });
  if (type === 'codex' || type === 'claude' || windows.length === 0) {
    return [
      primary ? { ...primary, label: '5h' } : placeholder('five-hour', '5h'),
      weekly ? { ...weekly, label: '7d' } : placeholder('weekly', '7d'),
      ...windows.filter((window) => window !== primary && window !== weekly),
    ];
  }
  return windows.map((window) => ({
    ...window,
    label: window.periodHours === 5 ? '5h' : window.periodHours === 168 ? '7d' : window.label,
  }));
}

export function usageResetText(
  window: UsageWindow,
  now: number,
  labels: { now: string; pending: string },
  showNowWhenIdle = true
): string {
  if (showNowWhenIdle && window.usedPercent !== null && window.usedPercent <= 0) return labels.now;
  const reset = finite(window.resetAtMs);
  if (reset === null) return '--';
  const diff = reset - now;
  if (diff <= 0)
    return window.usedPercent === null
      ? '--'
      : window.usedPercent <= 0
        ? labels.now
        : labels.pending;
  const minutes = Math.floor(diff / 60000);
  const hours = Math.floor(minutes / 60);
  if (hours >= 24) return `${Math.floor(hours / 24)}d ${hours % 24}h`;
  return hours > 0 ? `${hours}h ${minutes % 60}m` : `${minutes}m`;
}
