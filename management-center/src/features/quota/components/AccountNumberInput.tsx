import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import styles from './QuotaRow.module.scss';

export function AccountNumberInput({
  value,
  label,
  hint,
  disabled,
  max = 2147483647,
  onSave,
}: {
  value: number;
  label: string;
  hint: string;
  disabled: boolean;
  max?: number;
  onSave: (value: number) => Promise<void>;
}) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const pending = useRef(false);
  const save = async () => {
    if (pending.current || disabled || draft === null) return;
    const parsed = Number(draft);
    if (!draft.trim() || !Number.isSafeInteger(parsed) || parsed < 0 || parsed > max) {
      setError(t('quota_management.number_invalid', { max }));
      return;
    }
    if (parsed === value) {
      setDraft(null);
      setError('');
      return;
    }
    pending.current = true;
    setSaving(true);
    setError('');
    try {
      await onSave(parsed);
      setDraft(null);
    } catch (err) {
      setDraft(null);
      setError(err instanceof Error ? err.message : t('notification.save_failed'));
    } finally {
      pending.current = false;
      setSaving(false);
    }
  };
  return (
    <div className={styles.numberEditor}>
      <input
        type="number"
        min={0}
        max={max}
        step={1}
        value={draft ?? value}
        aria-label={label}
        title={hint}
        aria-invalid={Boolean(error)}
        aria-busy={saving}
        disabled={disabled || saving}
        className={styles.numberInput}
        onChange={(event) => {
          setDraft(event.target.value);
          setError('');
        }}
        onBlur={() => void save()}
        onKeyDown={(event) => {
          if (event.key === 'Enter') {
            event.preventDefault();
            event.currentTarget.blur();
          }
          if (event.key === 'Escape') {
            event.preventDefault();
            setDraft(null);
            setError('');
          }
        }}
      />
      {error && (
        <span className={styles.editError} role="alert">
          {error}
        </span>
      )}
    </div>
  );
}
