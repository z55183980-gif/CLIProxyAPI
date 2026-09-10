import { useTranslation } from 'react-i18next';
import { Modal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import type { AuthFileItem } from '@/types';
import { useAuthFileModelTests } from '../hooks/useAuthFileModelTests';
import type { AuthFileModelItem } from '@/features/authFiles/constants';
import { isModelExcluded } from '@/features/authFiles/constants';
import styles from './AuthFileModelsModal.module.scss';

export type AuthFileModelsModalProps = {
  open: boolean;
  fileName: string;
  authFile: AuthFileItem | null;
  fileType: string;
  loading: boolean;
  error: 'unsupported' | null;
  models: AuthFileModelItem[];
  excluded: Record<string, string[]>;
  onClose: () => void;
  onCopyText: (text: string) => void;
};

export function AuthFileModelsModal(props: AuthFileModelsModalProps) {
  const { t } = useTranslation();
  const {
    open,
    fileName,
    authFile,
    fileType,
    loading,
    error,
    models,
    excluded,
    onClose,
    onCopyText,
  } = props;
  const { results, testModel, disabled } = useAuthFileModelTests(open, authFile);

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t('auth_files.models_title', { defaultValue: '支持的模型' }) + ` - ${fileName}`}
      footer={
        <Button variant="secondary" onClick={onClose}>
          {t('common.close')}
        </Button>
      }
    >
      {loading ? (
        <div className="hint">
          {t('auth_files.models_loading', { defaultValue: '正在加载模型列表...' })}
        </div>
      ) : error === 'unsupported' ? (
        <EmptyState
          title={t('auth_files.models_unsupported', { defaultValue: '当前版本不支持此功能' })}
          description={t('auth_files.models_unsupported_desc', {
            defaultValue: '请更新 CLI Proxy API 到最新版本后重试',
          })}
        />
      ) : models.length === 0 ? (
        <EmptyState
          title={t('auth_files.models_empty', { defaultValue: '该凭证暂无可用模型' })}
          description={t('auth_files.models_empty_desc', {
            defaultValue: '该认证凭证可能尚未被服务器加载或没有绑定任何模型',
          })}
        />
      ) : (
        <div className={styles.list}>
          {models.map((model) => {
            const excludedModel = isModelExcluded(model.id, fileType, excluded);
            const result = results[model.id];
            return (
              <div
                key={model.id}
                className={`${styles.item} ${excludedModel ? styles.itemExcluded : ''}`}
              >
                <button
                  type="button"
                  className={styles.modelInfo}
                  onClick={() => onCopyText(model.id)}
                  title={t('common.copy')}
                >
                  <span className={styles.modelId}>{model.id}</span>
                  {model.display_name && model.display_name !== model.id && (
                    <span className={styles.modelDisplayName}>{model.display_name}</span>
                  )}
                  {model.type && <span className={styles.modelType}>{model.type}</span>}
                  {excludedModel && (
                    <span className={styles.excludedBadge}>
                      {t('auth_files.models_excluded_badge', { defaultValue: '已禁用' })}
                    </span>
                  )}
                </button>
                <div className={styles.testActions}>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={disabled || excludedModel || result?.status === 'testing'}
                    onClick={() => void testModel(model.id)}
                    aria-label={`${t('auth_files.test_connection')} ${model.id}`}
                  >
                    {result?.status === 'testing' && <LoadingSpinner size={14} />}
                    {t(
                      result?.status === 'testing'
                        ? 'auth_files.model_test_running'
                        : 'auth_files.test_connection'
                    )}
                  </Button>
                  {result && result.status !== 'testing' && (
                    <span
                      className={
                        result.status === 'success' ? styles.testSuccess : styles.testError
                      }
                      role="status"
                    >
                      {t(
                        result.status === 'success'
                          ? 'auth_files.test_connection_success'
                          : 'auth_files.test_connection_failed'
                      )}
                      {result.latencyMs != null && ` · ${result.latencyMs} ms`}
                      {result.error && `: ${result.error}`}
                    </span>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </Modal>
  );
}
