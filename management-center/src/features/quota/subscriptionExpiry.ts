import type { AuthFileItem, CodexQuotaState } from '@/types';
import { resolveCodexSubscriptionActiveUntil, resolveResetMs } from '@/utils/quota';
import type { QuotaCardState } from './providers';
import type { QuotaProviderType } from './providers/types';

const record = (value: unknown): Record<string, unknown> | null =>
  value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;

// Subscription dates only: OAuth exp/expires_at and quota resets have different meanings.
export function subscriptionExpiryMs(
  file: AuthFileItem,
  type: QuotaProviderType,
  quota?: QuotaCardState
): number | null {
  const candidates: unknown[] = [];
  if (type === 'codex') {
    if (quota?.status === 'success')
      candidates.push((quota as CodexQuotaState).subscriptionActiveUntil);
    candidates.push(resolveCodexSubscriptionActiveUntil(file));
  }
  for (const source of [
    file,
    record(file.credentials),
    record(file.metadata),
    record(file.attributes),
  ]) {
    if (!source) continue;
    const subscription = record(source.subscription);
    candidates.push(
      source.subscription_expires_at,
      source.subscriptionExpiresAt,
      source.subscription_active_until,
      source.subscriptionActiveUntil,
      subscription?.expires_at,
      subscription?.expiresAt,
      subscription?.active_until,
      subscription?.activeUntil
    );
  }
  const ms = resolveResetMs(candidates.filter((value) => value !== 0 && value !== '0'));
  return ms !== null && ms > 0 && Number.isFinite(new Date(ms).getTime()) ? ms : null;
}

export function formatSubscriptionExpiry(ms: number | null): string {
  if (ms === null) return '--';
  const date = new Date(ms);
  if (!Number.isFinite(date.getTime())) return '--';
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
}
