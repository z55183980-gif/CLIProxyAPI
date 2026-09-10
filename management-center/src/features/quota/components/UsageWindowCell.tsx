import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconRefreshCw } from '@/components/ui/icons';
import { useNow } from '@/hooks/useNow';
import { useAuthStore } from '@/stores';
import { usageHistoryApi, type AccountUsageStats } from '@/services/api/usageHistory';
import { resolveQuotaErrorMessage } from '@/utils/quota';
import type { CodexQuotaState } from '@/types';
import type { QuotaCardState } from '../providers';
import type { QuotaProviderType } from '../providers/types';
import { usageWindowsFor, usageResetText } from '../usageWindows';
import styles from './UsageWindowCell.module.scss';
import { ChannelUsageWindowCell } from './ChannelUsageWindowCell';

export interface UsageWindowCellProps {
  type: QuotaProviderType;
  accountId?: string | null;
  authIndex?: string | null;
  accountName?: string;
  refreshKey?: string;
  quota?: QuotaCardState;
  canRefresh: boolean;
  canReset: boolean;
  resetting: boolean;
  onRefresh: () => void;
  onReset: () => void;
}

export function UsageWindowCell(props: UsageWindowCellProps) {
  if (props.type === 'codex' || props.type === 'claude')
    return <ChannelUsageWindowCell {...props} />;
  return <OtherUsageWindowCell {...props} />;
}

function OtherUsageWindowCell({
  type,
  accountId,
  quota,
  canRefresh,
  canReset,
  resetting,
  onRefresh,
  onReset,
}: UsageWindowCellProps) {
  const { t } = useTranslation();
  const windows = usageWindowsFor(type, quota);
  const connected = useAuthStore((state) => state.connectionStatus === 'connected');
  const [windowStats, setWindowStats] = useState<AccountUsageStats | null>(null);
  const primary = windows[0];
  useEffect(() => {
    if (!connected || !accountId || !primary?.resetAtMs || !primary.periodHours) return;
    const controller = new AbortController();
    const end = new Date();
    const start = new Date(primary.resetAtMs - primary.periodHours * 3600000);
    void usageHistoryApi
      .accountTodayStats([accountId], start.toISOString(), end.toISOString(), controller.signal)
      .then((stats) => setWindowStats(stats[accountId] ?? null))
      .catch(() => undefined);
    return () => controller.abort();
  }, [accountId, connected, primary?.periodHours, primary?.resetAtMs]);
  const stat = (value: number | null | undefined) =>
    value == null ? '--' : new Intl.NumberFormat().format(value);
  const cost = (value: number | null | undefined) => (value == null ? '--' : value.toFixed(2));
  const now = useNow(windows.some((window) => window.resetAtMs != null));
  const loading = quota?.status === 'loading';
  const disabled = !canRefresh || loading || resetting;
  const credits =
    type === 'codex' && quota?.status === 'success'
      ? (quota as CodexQuotaState).rateLimitResetCreditsAvailableCount
      : null;
  const count = typeof credits === 'number' && Number.isFinite(credits) ? credits : '--';
  return (
    <div className={styles.cell} aria-busy={loading}>
      {windows.map((window, index) => {
        const used = window.usedPercent;
        const percent =
          used === null ? '--%' : used > 999 ? '>999%' : `${Math.round(Math.max(0, used))}%`;
        const tone =
          used !== null && used >= 100
            ? styles.danger
            : used !== null && used >= 80
              ? styles.warning
              : '';
        return (
          <div key={window.id} className={styles.window}>
            {index === 1 && (
              <div className={styles.stats} aria-label={t('quota_management.window_stats')}>
                <span title={t('quota_management.window_requests')}>
                  {stat(windowStats?.requests)} req
                </span>
                <span title={t('quota_management.window_tokens')}>{stat(windowStats?.tokens)}</span>
                <span title={t('quota_management.window_account_cost')}>
                  A ${cost(windowStats?.cost)}
                </span>
              </div>
            )}
            <div className={styles.progressRow}>
              <span
                className={`${styles.badge} ${index === 0 ? styles.indigo : styles.emerald}`}
                title={window.label}
              >
                {window.label}
              </span>
              <div
                className={`${styles.track} ${tone}`}
                role="progressbar"
                aria-label={t('quota_management.window_utilization', { window: window.label })}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={used === null ? undefined : Math.max(0, Math.min(100, used))}
                aria-valuetext={used === null ? t('quota_management.window_no_data') : percent}
              >
                <div
                  className={styles.fill}
                  style={{ width: `${Math.max(0, Math.min(100, used ?? 0))}%` }}
                />
              </div>
              <span className={`${styles.percent} ${tone}`}>{percent}</span>
              <span className={styles.resetTime}>
                {usageResetText(window, now, {
                  now: t('quota_management.window_now'),
                  pending: t('quota_management.window_pending'),
                })}
              </span>
            </div>
          </div>
        );
      })}
      <div className={styles.actions}>
        <button
          type="button"
          onClick={onRefresh}
          disabled={disabled}
          title={t('auth_files.quota_refresh_hint')}
        >
          <IconRefreshCw size={10} className={loading ? styles.spinning : undefined} />
          {t('quota_management.window_query')}
        </button>
        <button
          type="button"
          onClick={onRefresh}
          disabled={disabled || type !== 'codex'}
          title={t('codex_quota.reset_credits_label')}
        >
          <IconRefreshCw size={10} className={loading ? styles.spinning : undefined} />
          {t('quota_management.window_count')} {count}
        </button>
        <button
          type="button"
          className={styles.resetAction}
          onClick={onReset}
          disabled={disabled || !canReset}
          title={t('codex_quota.reset_button')}
        >
          <svg
            width="10"
            height="10"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            aria-hidden="true"
            className={resetting ? styles.spinning : undefined}
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M20 12a8 8 0 11-2.343-5.657L20 8m0 0V4m0 4h-4"
            />
          </svg>
          {t('quota_management.window_reset')}
        </button>
      </div>
      {quota?.status === 'error' && (
        <div className={styles.error} role="alert">
          {resolveQuotaErrorMessage(t, quota.errorStatus, quota.error || t('common.unknown_error'))}
        </div>
      )}
    </div>
  );
}
