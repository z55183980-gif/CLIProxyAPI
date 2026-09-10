import { useState, type CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import type { ResolvedTheme } from '@/types';
import {
  getAuthFileIcon,
  hasAuthFileStatusWarning,
  getAuthFileStatusMessage,
  getThemeSurfaceIconBackground,
  getTypeLabel,
  isThemeSurfaceIconProvider,
} from '@/features/authFiles/constants';
import { deriveAuthFileIdentity } from '@/features/authFiles/identity';
import { QUOTA_ADAPTERS, type QuotaCardState } from '../providers';
import { type QuotaFileEntry } from '../logic';
import styles from './QuotaRow.module.scss';
import { AccountTodayStatsCell } from './AccountTodayStatsCell';
import { todayStatsAccountId, type AccountTodayStats } from '../todayStats';
import { subscriptionExpiryMs, formatSubscriptionExpiry } from '../subscriptionExpiry';
import { UsageWindowCell } from './UsageWindowCell';
import { normalizeAuthIndex } from '@/utils/authIndex';

import { AccountNumberInput } from './AccountNumberInput';

export type QuotaRowProps = {
  entry: QuotaFileEntry;
  quota?: QuotaCardState;
  todayStats?: AccountTodayStats;
  todayStatsError?: string;
  todayStatsLoading?: boolean;
  resolvedTheme: ResolvedTheme;
  canRefresh: boolean;
  resetting: boolean;

  entranceDelayMs?: number | null;
  onRefresh: () => void;
  onReset: () => void;
  usageRevision?: number;
  canEdit?: boolean;
  onSaveNumber?: (field: 'concurrency' | 'priority' | 'weight', value: number) => Promise<void>;
};

export function QuotaRow(props: QuotaRowProps) {
  const {
    entry,
    quota,
    todayStats,
    todayStatsError,
    todayStatsLoading,
    resolvedTheme,
    canRefresh,
    resetting,
    entranceDelayMs,
    onRefresh,
    onReset,
  } = props;
  const { t } = useTranslation();
  const adapter = QUOTA_ADAPTERS[entry.type];
  const file = entry.file;
  const identity = deriveAuthFileIdentity(file);
  const subscriptionExpiry = subscriptionExpiryMs(file, entry.type, quota);
  const warning = hasAuthFileStatusWarning(file);
  const stateLabel = t(
    file.disabled
      ? 'auth_files.health_status_disabled'
      : warning
        ? 'auth_files.health_status_warning'
        : 'auth_files.health_status_healthy'
  );

  // Preserve the initial entrance delay across updates.
  const [mountEntranceDelayMs] = useState<number | null>(entranceDelayMs ?? null);
  const entranceStyle =
    mountEntranceDelayMs === null
      ? undefined
      : ({ '--row-delay': `${mountEntranceDelayMs}ms` } as CSSProperties);

  const status = quota?.status ?? 'idle';
  const iconSrc = getAuthFileIcon(entry.type, resolvedTheme);
  const typeLabel = getTypeLabel(t, entry.type);
  const showReset =
    status === 'success' &&
    Boolean(adapter.resetQuota) &&
    quota !== undefined &&
    Boolean(adapter.canResetQuota?.(quota));

  return (
    <tr
      className={`${styles.row} ${mountEntranceDelayMs === null ? '' : styles.rowEnter}`}
      style={entranceStyle}
    >
      <td>
        <div className={styles.identity}>
          <span className={styles.account} title={identity.primary}>
            {identity.primary}
          </span>
          {identity.secondary && (
            <span className={styles.fileName} title={file.name}>
              {identity.secondary}
            </span>
          )}
          <span
            className={`${styles.subscriptionExpiry} ${subscriptionExpiry === null ? styles.subscriptionUnknown : ''}`}
          >
            {t('quota_management.subscription_expiry', {
              date: formatSubscriptionExpiry(subscriptionExpiry),
            })}
          </span>
          {typeof file.note === 'string' && file.note.trim() && (
            <span className={styles.note} title={file.note}>
              {file.note}
            </span>
          )}
        </div>
      </td>
      <td>
        <div className={styles.head}>
          <span
            className={styles.iconWrap}
            title={typeLabel}
            style={
              isThemeSurfaceIconProvider(entry.type)
                ? { background: getThemeSurfaceIconBackground(resolvedTheme) }
                : undefined
            }
          >
            {iconSrc ? (
              <img src={iconSrc} alt="" className={styles.icon} />
            ) : (
              <span className={styles.iconFallback}>{typeLabel.slice(0, 1).toUpperCase()}</span>
            )}
          </span>
          <span className={styles.providerLabel}>{typeLabel}</span>
        </div>
      </td>
      <td>
        <span
          className={`${styles.stateBadge} ${file.disabled ? styles.stateDisabled : warning ? styles.stateWarning : styles.stateActive}`}
        >
          {stateLabel}
        </span>
        {warning && (
          <div className={styles.warning} title={getAuthFileStatusMessage(file)}>
            {getAuthFileStatusMessage(file)}
          </div>
        )}
      </td>
      <td>
        <AccountTodayStatsCell
          stats={todayStats}
          error={todayStatsError}
          loading={todayStatsLoading}
        />
      </td>
      <td className={styles.usageCell}>
        <UsageWindowCell
          authIndex={normalizeAuthIndex(file.authIndex ?? file.auth_index)}
          accountName={file.name}
          refreshKey={`${props.usageRevision ?? 0}:${JSON.stringify(file.quota ?? {})}`}
          accountId={todayStatsAccountId(file)}
          type={entry.type}
          quota={quota}
          canRefresh={canRefresh}
          canReset={showReset}
          resetting={resetting}
          onRefresh={onRefresh}
          onReset={onReset}
        />
      </td>
      <td>
        <div
          className={`${styles.capacity} ${file.concurrency && (file.currentConcurrency ?? 0) >= file.concurrency ? styles.capacityFull : (file.currentConcurrency ?? 0) > 0 ? styles.capacityBusy : ''}`}
        >
          <span className={styles.capacityCount} title={t('quota_management.capacity_hint')}>
            {file.currentConcurrency ?? '--'} /
          </span>
          <AccountNumberInput
            value={file.concurrency ?? 0}
            max={1000000}
            label={t('quota_management.capacity_label', { name: identity.primary })}
            hint={t('quota_management.capacity_hint')}
            disabled={!props.canEdit || file.capacityEditable !== true}
            onSave={(value) => props.onSaveNumber!('concurrency', value)}
          />
        </div>
      </td>
      <td>
        <AccountNumberInput
          value={file.priority ?? 0}
          label={t('quota_management.priority_label', { name: identity.primary })}
          hint={t('quota_management.priority_hint')}
          disabled={!props.canEdit || file.runtimeOnly === true || file.capacityEditable === false}
          onSave={(value) => props.onSaveNumber!('priority', value)}
        />
      </td>
      <td>
        <AccountNumberInput
          value={Math.max(0, file.weight ?? 1)}
          max={1000000}
          label={t('quota_management.weight_label', { name: identity.primary })}
          hint={t('quota_management.weight_hint')}
          disabled={!props.canEdit || file.runtimeOnly === true || file.capacityEditable === false}
          onSave={(value) => props.onSaveNumber!('weight', value)}
        />
      </td>
    </tr>
  );
}
