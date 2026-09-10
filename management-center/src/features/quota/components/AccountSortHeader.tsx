import { useTranslation } from 'react-i18next';
import {
  nextAccountColumnSort,
  type AccountColumnSort,
  type AccountSortField,
} from '../columnSort';
import styles from '../QuotaPage.module.scss';

export function AccountSortHeader({
  field,
  label,
  className,
  sort,
  onSort,
}: {
  field: AccountSortField;
  label: string;
  className: string;
  sort: AccountColumnSort;
  onSort: (field: AccountSortField) => void;
}) {
  const { t } = useTranslation();
  const direction = sort?.field === field ? sort.direction : 'none';
  const next = nextAccountColumnSort(sort, field)?.direction ?? 'none';
  const action = t(`quota_management.column_sort_${next}`, { label });
  return (
    <th scope="col" className={className} aria-sort={direction}>
      <button
        type="button"
        className={styles.sortHeader}
        onClick={() => onSort(field)}
        title={action}
        aria-label={action}
      >
        {label}
        <span aria-hidden="true" className={styles.sortIndicator}>
          {direction === 'ascending' ? '↑' : direction === 'descending' ? '↓' : '↕'}
        </span>
      </button>
    </th>
  );
}
