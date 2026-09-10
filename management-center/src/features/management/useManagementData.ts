import { useCallback, useEffect, useRef, useState } from 'react';
import { useAuthStore } from '@/stores';

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
  const lastRefreshAt = useRef(0);
  const refresh = useCallback(async () => {
    // Protect the management API from accidental refresh storms when several
    // components trigger the shared header refresh at the same time.
    const now = Date.now();
    if (pending.current || now - lastRefreshAt.current < 1000) return;
    lastRefreshAt.current = now;
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
    return () => {
      controller.current?.abort();
    };
  }, [refresh, apiBase, key]);
  return { data, loading, error, refresh };
}
