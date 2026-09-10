import { useTranslation } from 'react-i18next';
import { formatTodayTokens, type AccountTodayStats } from '../todayStats';
import styles from './AccountTodayStatsCell.module.scss';

export interface AccountTodayStatsCellProps {
  stats?: AccountTodayStats;
  loading?: boolean;
  error?: string;
}

export function AccountTodayStatsCell({
  stats,
  loading = false,
  error,
}: AccountTodayStatsCellProps) {
  const { t, i18n } = useTranslation();
  const locale = i18n.resolvedLanguage || 'zh-CN';
  const number = (value: number | null | undefined) =>
    value == null ? '--' : new Intl.NumberFormat(locale).format(value);
  const money = (value: number | null | undefined) =>
    value == null
      ? '--'
      : new Intl.NumberFormat(locale, {
          style: 'currency',
          currency: 'USD',
          minimumFractionDigits: value > 0 && value < 0.01 ? 6 : 2,
          maximumFractionDigits: value > 0 && value < 0.01 ? 6 : 2,
        }).format(value);
  const rows = [
    { key: 'today_requests', value: number(stats?.requests) },
    { key: 'today_tokens', value: formatTodayTokens(stats?.tokens ?? null) },
    {
      key: 'today_account_cost',
      value: money(stats?.accountCost),
      account: true,
      hint: stats?.unpriced
        ? t('quota_management.today_unpriced_hint', { count: stats.unpriced })
        : stats?.estimated
          ? t('quota_management.today_estimated_hint', { count: stats.estimated })
          : t('quota_management.today_cost_hint'),
    },
  ];
  return (
    <div
      className={styles.cell}
      aria-busy={loading}
      title={error ? t('quota_management.today_error', { message: error }) : undefined}
    >
      {rows.map((row) => (
        <div className={styles.line} key={row.key} title={row.hint}>
          <span className={styles.label}>{t(`quota_management.${row.key}`)}:</span>
          <span className={row.account ? styles.accountCost : styles.value}>{row.value}</span>
        </div>
      ))}
      {error && (
        <span className={styles.srOnly} role="status">
          {t('quota_management.today_error', { message: error })}
        </span>
      )}
    </div>
  );
}
