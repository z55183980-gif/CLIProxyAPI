import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IconRefreshCw } from '@/components/ui/icons';
import { useNow } from '@/hooks/useNow';
import { useAuthStore, useQuotaStore } from '@/stores';
import { accountUsageWindowsApi, type AccountUsageWindows } from '@/services/api/usageWindows';
import type { CodexQuotaState } from '@/types';
import { usageResetText } from '../usageWindows';
import { channelUsageCache } from '../channelUsageCache';
import type { UsageWindowCellProps } from './UsageWindowCell';
import styles from './UsageWindowCell.module.scss';

export function ChannelUsageWindowCell({
  type,
  authIndex,
  accountName,
  refreshKey,
  quota,
  canRefresh,
  canReset,
  resetting,
  onRefresh,
  onReset,
}: UsageWindowCellProps) {
  const { t } = useTranslation();
  const connected = useAuthStore((state) => state.connectionStatus === 'connected');
  const generation = useQuotaStore((state) => state.cacheGeneration);
  const [data, setData] = useState<AccountUsageWindows | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const request = useRef<AbortController | null>(null);
  const [showExpiries, setShowExpiries] = useState(false);
  const load = useCallback(
    async (active: boolean, refreshStats = false) => {
      if (refreshStats && request.current && !request.current.signal.aborted) return;
      request.current?.abort();
      if (!connected || !authIndex || !canRefresh) return;
      const key = `${type}:${authIndex}`;
      const revision = refreshKey ?? '';
      const cached =
        !active && !refreshStats && channelUsageCache.get(generation, key, revision, Date.now());
      if (cached) {
        setData(cached);
        setLoading(false);
        return;
      }
      const controller = new AbortController();
      request.current = controller;
      setLoading(true);
      setError('');
      try {
        const result = await accountUsageWindowsApi.get(authIndex, active, controller.signal);
        if (!controller.signal.aborted && useQuotaStore.getState().cacheGeneration === generation) {
          channelUsageCache.set(generation, key, revision, result, Date.now());
          setData(result);
          if (accountName) {
            const windows = result.windows.map((window) => ({
              id: window.id,
              label: window.label,
              usedPercent: window.usedPercent,
              resetAtMs: window.resetAt ? Date.parse(window.resetAt) : null,
              periodHours: window.id === 'five-hour' ? 5 : 168,
              resetLabel: window.resetAt ? new Date(window.resetAt).toLocaleString() : '-',
            }));
            const store = useQuotaStore.getState();
            if (type === 'claude') {
              store.setClaudeQuota((previous) => ({
                ...previous,
                [accountName]: { ...previous[accountName], status: 'success', windows },
              }));
            } else {
              store.setCodexQuota((previous) =>
                previous[accountName]?.status === 'loading'
                  ? previous
                  : {
                      ...previous,
                      [accountName]: { ...previous[accountName], status: 'success', windows },
                    }
              );
            }
          }
        }
      } catch (err) {
        if (!controller.signal.aborted) {
          setError(err instanceof Error ? err.message : t('common.unknown_error'));
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false);
        if (request.current === controller) request.current = null;
      }
    },
    [authIndex, accountName, canRefresh, connected, t, type, generation, refreshKey]
  );

  useEffect(() => {
    setData(null);
    setError('');
    setLoading(false);
    setShowExpiries(false);
    void load(false);
    return () => request.current?.abort();
  }, [load, generation, refreshKey]);

  const now = useNow(connected && canRefresh);
  const lastStatsTick = useRef(now);
  useEffect(() => {
    if (lastStatsTick.current === now) return;
    lastStatsTick.current = now;
    // Refresh local statistics even when quota percentages and reset times are unchanged.
    void load(false, true);
  }, [now, load]);
  const credits =
    type === 'codex' && quota?.status === 'success' ? (quota as CodexQuotaState) : undefined;
  const creditLoading = quota?.status === 'loading';
  const expiries = (credits?.rateLimitResetCredits ?? [])
    .map((credit) => credit.expiresAt)
    .filter(Boolean)
    .sort((a, b) => Date.parse(a) - Date.parse(b));
  const count = credits?.rateLimitResetCreditsAvailableCount;
  const windowError = error || data?.error || data?.statsError;

  return (
    <div className={styles.cell} aria-busy={loading}>
      {!data && <span className={styles.resetTime}>{loading ? t('common.loading') : '-'}</span>}
      {data && <ChannelWindowRows data={data} type={type} now={now} />}
      <div className={styles.actions}>
        {type === 'claude' && data?.source === 'passive' && (
          <span className={styles.resetTime}>{t('quota_management.window_passive')}</span>
        )}
        <button
          type="button"
          onClick={() => void load(true)}
          disabled={!canRefresh || loading}
          title={t('quota_management.window_query_hint')}
        >
          <IconRefreshCw size={10} className={loading ? styles.spinning : undefined} />
          {t('quota_management.window_query')}
        </button>
        {type === 'codex' && (
          <>
            <button
              type="button"
              onClick={onRefresh}
              disabled={!canRefresh || creditLoading || resetting}
              title={t('codex_quota.reset_credits_label')}
            >
              <IconRefreshCw size={10} className={creditLoading ? styles.spinning : undefined} />
              {t('quota_management.window_count')}
              {count != null ? ` ${count}` : ''}
            </button>
            <button
              type="button"
              onClick={onReset}
              className={styles.resetAction}
              disabled={!canRefresh || creditLoading || resetting || !canReset}
              title={t('codex_quota.reset_button')}
            >
              <IconRefreshCw size={10} className={resetting ? styles.spinning : undefined} />
              {t('quota_management.window_reset')}
            </button>
          </>
        )}
      </div>
      {type === 'codex' && expiries.length > 0 && (
        <div className={styles.resetTime}>
          {t('quota_management.window_credit_expiry', {
            time: new Date(expiries[0]).toLocaleString(),
          })}
          {expiries.length > 1 && (
            <button
              type="button"
              aria-expanded={showExpiries}
              onClick={() => setShowExpiries((current) => !current)}
              aria-label={t('quota_management.window_credit_details')}
            >
              +{expiries.length - 1}
            </button>
          )}
          {showExpiries &&
            expiries
              .slice(1)
              .map((expiry, index) => (
                <div key={`${expiry}:${index}`}>{new Date(expiry).toLocaleString()}</div>
              ))}
        </div>
      )}
      {windowError && (
        <div className={styles.error} role="alert">
          {windowError}
        </div>
      )}
      {type === 'codex' && quota?.status === 'error' && (
        <div className={styles.error} role="alert">
          {quota.error}
        </div>
      )}
      {credits?.rateLimitResetCreditsError && (
        <div className={styles.error} role="alert">
          {credits.rateLimitResetCreditsError}
        </div>
      )}
    </div>
  );
}

export function ChannelWindowRows({
  data,
  type,
  now,
}: {
  data: AccountUsageWindows;
  type: string;
  now: number;
}) {
  const { t } = useTranslation();
  const compact = (value: number, allowBillions = true): string => {
    if (allowBillions && value >= 1e9) return `${(value / 1e9).toFixed(1)}B`;
    if (value >= 1e6) return `${(value / 1e6).toFixed(1)}M`;
    if (value >= 1e3) return `${(value / 1e3).toFixed(1)}K`;
    return String(value);
  };
  return (
    <>
      {data.windows.map((window) => {
        const used = window.usedPercent;
        const percent = Math.round(used) > 999 ? '>999%' : `${Math.round(used)}%`;
        const tone = used >= 100 ? styles.danger : used >= 80 ? styles.warning : '';
        const stats = window.stats;
        const resetAtMs = window.resetAt ? Date.parse(window.resetAt) : null;
        const reset = usageResetText(
          { ...window, resetAtMs },
          now,
          {
            now: t('quota_management.window_now'),
            pending: t('quota_management.window_pending'),
          },
          type === 'codex'
        );
        const color =
          window.id === 'seven-day-sonnet'
            ? styles.purple
            : window.id === 'seven-day-fable'
              ? styles.amber
              : window.id === 'five-hour'
                ? styles.indigo
                : styles.emerald;
        return (
          <div key={window.id} className={styles.window}>
            {stats && (stats.requests > 0 || stats.tokens > 0) && (
              <div className={styles.stats} aria-label={t('quota_management.window_stats')}>
                <span title={t('quota_management.window_requests')}>
                  {compact(stats.requests, false)} req
                </span>
                <span title={t('quota_management.window_tokens')}>{compact(stats.tokens)}</span>
                <span title={t('quota_management.window_account_cost')}>
                  A ${stats.unpriced > 0 ? '--' : stats.cost.toFixed(2)}
                </span>
              </div>
            )}
            <div className={styles.progressRow}>
              <span className={`${styles.badge} ${color}`}>{window.label}</span>
              <div
                className={`${styles.track} ${tone}`}
                role="progressbar"
                aria-label={t('quota_management.window_utilization', { window: window.label })}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={Math.min(100, Math.max(0, used))}
                aria-valuetext={percent}
              >
                <div
                  className={styles.fill}
                  style={{ width: `${Math.min(100, Math.max(0, used))}%` }}
                />
              </div>
              <span className={`${styles.percent} ${tone}`}>{percent}</span>
              {(window.resetAt || (type === 'codex' && used <= 0)) && (
                <span className={styles.resetTime}>{reset}</span>
              )}
            </div>
          </div>
        );
      })}
    </>
  );
}
