import { useState, type CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import { SelectionCheckbox } from '@/components/ui/SelectionCheckbox';
import {
  IconInfo,
} from '@/components/ui/icons';
import { ProviderStatusBar } from '@/components/providers/ProviderStatusBar';
import type { AuthFileItem } from '@/types';
import { resolveAuthProvider } from '@/utils/quota';
import { statusBarDataFromRecentRequests } from '@/utils/recentRequests';
import {
  QUOTA_PROVIDER_TYPES,
  getAuthFileIcon,
  getAuthFileStatusMessage,
  getThemeSurfaceIconBackground,
  hasAuthFileStatusWarning,
  getTypeColor,
  getTypeLabel,
  isRuntimeOnlyAuthFile,
  isThemeSurfaceIconProvider,
  normalizeProviderKey,
  type QuotaProviderType,
  type ResolvedTheme,
} from '@/features/authFiles/constants';
import { deriveAuthFileIdentity } from '@/features/authFiles/identity';
import type { AuthFileStatusBarData } from '@/features/authFiles/hooks/useAuthFilesStatusBarCache';
import { AuthFileQuotaSection } from '@/features/authFiles/components/AuthFileQuotaSection';
import styles from './AuthFileCard.module.scss';

export type AuthFileCardProps = {
  file: AuthFileItem;
  compact: boolean;
  selected: boolean;
  resolvedTheme: ResolvedTheme;
  disableControls: boolean;
  deleting: string | null;
  statusUpdating: Record<string, boolean>;
  manualRefreshing: Record<string, boolean>;
  quotaFilterType: QuotaProviderType | null;
  statusBarCache: Map<string, AuthFileStatusBarData>;
  /** 首屏一次性级联入场的延迟；null/undefined 表示不做入场动画。 */
  entranceDelayMs?: number | null;
  onShowModels: (file: AuthFileItem) => void;
  onDownload: (name: string) => void;
  onManualRefresh: (file: AuthFileItem) => void;
  onOpenPrefixProxyEditor: (file: AuthFileItem) => void;
  onDelete: (name: string) => void;
  onToggleStatus: (file: AuthFileItem, enabled: boolean) => void;
  onToggleSelect: (name: string) => void;
};

const resolveQuotaType = (file: AuthFileItem): QuotaProviderType | null => {
  const provider = resolveAuthProvider(file);
  if (!QUOTA_PROVIDER_TYPES.has(provider as QuotaProviderType)) return null;
  return provider as QuotaProviderType;
};

export function AuthFileCard(props: AuthFileCardProps) {
  const { t } = useTranslation();
  const {
    file,
    compact,
    selected,
    resolvedTheme,
    disableControls,
    quotaFilterType,
    statusBarCache,
    entranceDelayMs,
    onToggleSelect,
  } = props;

  const isRuntimeOnly = isRuntimeOnlyAuthFile(file);
  const providerKey = normalizeProviderKey(String(file.type ?? file.provider ?? 'unknown'));
  const typeColor = getTypeColor(providerKey, resolvedTheme);
  const typeLabel = getTypeLabel(t, providerKey);
  const providerIcon = getAuthFileIcon(providerKey, resolvedTheme);
  // 与 AI 提供商界面一致：Kimi 图标底座随主题切换颜色
  const useThemeSurfaceIcon = isThemeSurfaceIconProvider(providerKey);

  const quotaType =
    quotaFilterType && resolveQuotaType(file) === quotaFilterType ? quotaFilterType : null;
  const showQuotaLayout = Boolean(quotaType) && !isRuntimeOnly && !compact;

  const successCount = file.successCount ?? 0;
  const failureCount = file.failureCount ?? 0;
  const authIndexKey = typeof file.authIndex === 'string' ? file.authIndex : null;
  const statusData =
    (authIndexKey && statusBarCache.get(authIndexKey)) ||
    statusBarDataFromRecentRequests(file.recentRequests ?? []);

  const rawStatusMessage = getAuthFileStatusMessage(file);
  const hasStatusWarning = hasAuthFileStatusWarning(file);

  const priorityValue = Number.isSafeInteger(file.priority) ? file.priority : 1;
  const currentCapacity = Number.isFinite(Number(file.currentConcurrency ?? file.current_concurrency)) ? Number(file.currentConcurrency ?? file.current_concurrency) : 0;
  const configuredCapacity = Number(file.concurrency ?? file.maxConcurrency ?? file.max_concurrency);
  const maxCapacity = Number.isFinite(configuredCapacity) && configuredCapacity > 0 ? configuredCapacity : 5;
  const noteValue = typeof file.note === 'string' ? file.note.trim() : '';
  // 主行显示账号（email/项目 ID），文件名降为满卡宽的 mono 副行
  const identity = deriveAuthFileIdentity(file);

  const stateLabel = isRuntimeOnly
    ? t('auth_files.type_virtual')
    : file.disabled
      ? t('auth_files.health_status_disabled')
      : hasStatusWarning
        ? t('auth_files.health_status_warning')
        : rawStatusMessage
          ? t('auth_files.health_status_healthy')
          : t('auth_files.status_toggle_label');
  const stateBadgeClass = isRuntimeOnly
    ? styles.stateVirtual
    : file.disabled
      ? styles.stateDisabled
      : hasStatusWarning
        ? styles.stateWarning
        : styles.stateActive;

  // 挂载时捕获一次入场延迟：父级随后传 null 也不会中断已开始的动画
  const [mountEntranceDelayMs] = useState<number | null>(entranceDelayMs ?? null);
  const cardClasses = [
    styles.card,
    compact ? styles.cardCompact : '',
    selected ? styles.cardSelected : '',
    file.disabled ? styles.cardDisabled : '',
    mountEntranceDelayMs != null ? styles.cardEnter : '',
  ]
    .filter(Boolean)
    .join(' ');
  const cardStyle =
    mountEntranceDelayMs != null
      ? ({ '--card-delay': `${mountEntranceDelayMs}ms` } as CSSProperties)
      : undefined;

  return (
    <article className={cardClasses} style={cardStyle}>
      <header className={styles.head}>
        {!isRuntimeOnly && (
          <SelectionCheckbox
            checked={selected}
            onChange={() => onToggleSelect(file.name)}
            className={styles.selection}
            aria-label={
              selected ? t('auth_files.batch_deselect') : t('auth_files.batch_select_all')
            }
            title={selected ? t('auth_files.batch_deselect') : t('auth_files.batch_select_all')}
          />
        )}
        <div
          className={styles.avatar}
          style={
            useThemeSurfaceIcon
              ? {
                  backgroundColor: getThemeSurfaceIconBackground(resolvedTheme),
                  color: typeColor.text,
                }
              : {
                  backgroundColor: typeColor.bg,
                  color: typeColor.text,
                  ...(typeColor.border ? { border: typeColor.border } : {}),
                }
          }
        >
          {providerIcon ? (
            <img src={providerIcon} alt="" className={styles.avatarImage} />
          ) : (
            <span className={styles.avatarFallback}>{typeLabel.slice(0, 1).toUpperCase()}</span>
          )}
        </div>
        <div className={styles.identity}>
          <div className={styles.badgeRow}>
            <span
              className={styles.typeBadge}
              style={{
                backgroundColor: typeColor.bg,
                color: typeColor.text,
                ...(typeColor.border ? { border: typeColor.border } : {}),
              }}
            >
              {typeLabel}
            </span>
            <span className={`${styles.stateBadge} ${stateBadgeClass}`}>
              <span className={styles.stateDot} aria-hidden="true" />
              {stateLabel}
            </span>
          </div>
          <span
            className={`${styles.account} ${identity.kind === 'fileName' ? styles.accountMono : ''}`}
            title={identity.primary}
          >
            {identity.primary}
          </span>
        </div>
      </header>

      {identity.secondary && (
        <p className={styles.fileName} title={identity.fullName}>
          {identity.secondary}
        </p>
      )}

      {!compact && noteValue && (
        <p className={styles.note} title={noteValue}>
          {noteValue}
        </p>
      )}

      {rawStatusMessage && hasStatusWarning && (
        <div className={styles.warning} title={rawStatusMessage}>
          <IconInfo className={styles.warningIcon} size={14} />
          <span>{rawStatusMessage}</span>
        </div>
      )}

      <div className={styles.health}>
        <div className={styles.healthHead}>
          <span className={styles.healthLabel}>{t('auth_files.health_status_label')}</span>
          <span className={styles.healthCounts}>
            <span
              className={`${styles.countOk} ${successCount > 0 ? styles.countLive : ''}`}
              title={t('stats.success')}
            >
              {t('stats.success')} {successCount}
            </span>
            <span
              className={`${styles.countFail} ${failureCount > 0 ? styles.countLive : ''}`}
              title={t('stats.failure')}
            >
              {t('stats.failure')} {failureCount}
            </span>
          </span>
        </div>
        <ProviderStatusBar statusData={statusData} styles={styles} />
      </div>

      <div className={styles.metaRow}>
        <span title={t('auth_files.capacity_display')}>{t('auth_files.capacity_display')}: {currentCapacity} / {maxCapacity}</span>
        <span className={styles.metaDivider} aria-hidden="true">·</span>
        <span className={styles.metaPriority} title={t('auth_files.priority_hint')}>
          <span className={styles.metaMetricLabel}>{t('auth_files.priority_display')}</span>
          <span>{priorityValue}</span>
        </span>
      </div>

      {showQuotaLayout && quotaType && (
        <AuthFileQuotaSection file={file} quotaType={quotaType} disableControls={disableControls} />
      )}

    </article>
  );
}
