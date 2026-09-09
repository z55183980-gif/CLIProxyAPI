import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { accountsApi, type AccountSummary } from '@/services/api/accounts';
import { useManagementData } from './useManagementData';
import styles from './Management.module.scss';

export function AccountsPage() {
  const { t } = useTranslation();
  const { data, loading, error, refresh } = useManagementData<AccountSummary[]>(
    accountsApi.list,
    []
  );
  const value = (n: number | null) =>
    n === null ? t('management.unavailable') : n.toLocaleString();
  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>{t('nav.accounts')}</h1>
          <p>{t('nav_meta.accounts')}</p>
        </div>
        <div className={styles.toolbar}>
          <Button variant="secondary" loading={loading} onClick={() => void refresh()}>
            {t('management.refresh')}
          </Button>
          <Link className="btn btn-primary" to="/oauth">
            {t('management.addAccount')}
          </Link>
        </div>
      </header>
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      {!loading && !error && !data.length && <p>{t('management.empty')}</p>}
      <div className={styles.grid}>
        {data.map((account) => (
          <Card
            key={account.id}
            title={<span className={styles.name}>{account.name}</span>}
            extra={t(
              `management.${['active', 'enabled'].includes(account.status) ? 'active' : 'inactive'}`
            )}
          >
            {account.message && <p className={styles.error}>{account.message}</p>}
            <dl className={styles.stats}>
              {(
                [
                  'fiveHour',
                  'sevenDay',
                  'rpm',
                  'tpm',
                  'concurrency',
                  'requests',
                  'tokens',
                  'cost',
                ] as const
              ).map((key) => (
                <div key={key}>
                  <dt>{t(`management.${key}`)}</dt>
                  <dd>
                    {account[key] === null
                      ? t('management.unavailable')
                      : key === 'cost'
                        ? `$${account.cost!.toFixed(2)}`
                        : key === 'fiveHour' || key === 'sevenDay'
                          ? `${account[key]!.toFixed(1)}%`
                          : value(account[key])}
                  </dd>
                </div>
              ))}
            </dl>
          </Card>
        ))}
      </div>
    </div>
  );
}
