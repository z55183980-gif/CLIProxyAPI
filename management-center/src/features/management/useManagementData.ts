import { useCallback, useEffect, useRef, useState } from 'react';
import { useAuthStore } from '@/stores';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';

/** Discard in-flight responses on navigation, logout, or connection changes. */
export function useManagementData<T>(loader: (signal: AbortSignal) => Promise<T>, initial: T) {
  const apiBase = useAuthStore((state) => state.apiBase);
  const key = useAuthStore((state) => state.managementKey);
  const [data, setData] = useState(initial);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const initialData = useRef(initial);
  const controller = useRef<AbortController | null>(null);
  const pending = useRef(false);
  const refresh = useCallback(async () => {
    controller.current?.abort();
    const request = new AbortController();
    controller.current = request;
    pending.current = true;
    setLoading(true);
    setError('');
    try {
      const result = await loader(request.signal);
      if (!request.signal.aborted) setData(result);
    } catch (err) {
      if (!request.signal.aborted) setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (!request.signal.aborted) {
        pending.current = false;
        setLoading(false);
      }
    }
  }, [loader]);
  useEffect(() => {
    setData(initialData.current);
    void refresh();
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible' && !pending.current) void refresh();
    }, 10000);
    return () => {
      controller.current?.abort();
      window.clearInterval(timer);
    };
  }, [refresh, apiBase, key]);
  useHeaderRefresh(refresh);
  return { data, loading, error, refresh };
}
