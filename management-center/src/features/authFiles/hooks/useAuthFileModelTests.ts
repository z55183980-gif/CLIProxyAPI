import { useCallback, useEffect, useRef, useState } from 'react';
import { authFilesApi } from '@/services/api';
import { useAuthStore } from '@/stores';
import type { AuthFileItem } from '@/types';

export type ModelTestState = {
  status: 'testing' | 'success' | 'error';
  latencyMs?: number;
  error?: string;
};

export function useAuthFileModelTests(open: boolean, file: AuthFileItem | null) {
  const [results, setResults] = useState<Record<string, ModelTestState>>({});
  const pending = useRef(new Map<string, AbortController>());
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const connectionStatus = useAuthStore((state) => state.connectionStatus);
  const disabled =
    !open ||
    !file ||
    file.disabled === true ||
    file.authIndex == null ||
    String(file.authIndex).trim() === '' ||
    connectionStatus !== 'connected';

  useEffect(() => {
    const requests = pending.current;
    requests.forEach((controller) => controller.abort());
    requests.clear();
    setResults({});
    return () => {
      requests.forEach((controller) => controller.abort());
      requests.clear();
    };
  }, [open, file?.name, file?.authIndex, apiBase, managementKey, connectionStatus]);

  const testModel = useCallback(
    async (model: string) => {
      if (disabled || !file || pending.current.has(model)) return;
      const controller = new AbortController();
      pending.current.set(model, controller);
      setResults((previous) => ({ ...previous, [model]: { status: 'testing' } }));
      try {
        const result = await authFilesApi.testModel(
          file.name,
          String(file.authIndex),
          model,
          controller.signal
        );
        if (controller.signal.aborted) return;
        setResults((previous) => ({
          ...previous,
          [model]: {
            status: result.success ? 'success' : 'error',
            latencyMs: result.latency_ms,
            error: result.error
              ? `${result.status_code ? `${result.status_code} ` : ''}${result.error}`
              : undefined,
          },
        }));
      } catch (error: unknown) {
        if (!controller.signal.aborted) {
          setResults((previous) => ({
            ...previous,
            [model]: {
              status: 'error',
              error: error instanceof Error ? error.message : undefined,
            },
          }));
        }
      } finally {
        if (pending.current.get(model) === controller) pending.current.delete(model);
      }
    },
    [disabled, file]
  );

  return { results, testModel, disabled };
}
