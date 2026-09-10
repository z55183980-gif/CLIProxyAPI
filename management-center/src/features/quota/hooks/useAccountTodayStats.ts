import { useEffect, useState } from 'react';
import { useAuthStore } from '@/stores';
import { useNow } from '@/hooks/useNow';
import { usageHistoryApi } from '@/services/api/usageHistory';
import { todayStatsFilters, todayStatsFromUsage, type AccountTodayStats } from '../todayStats';

interface StatsEntry {
  stats?: AccountTodayStats;
  error?: string;
}

export function useAccountTodayStats(accountIds: string[], revision: number) {
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const connected = useAuthStore((state) => state.connectionStatus === 'connected');
  const tick = useNow(connected);
  const now = new Date(tick);
  const day = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
  const idsKey = JSON.stringify([...new Set(accountIds)].sort());
  const scope = JSON.stringify([apiBase, managementKey, connected, day, idsKey]);
  const [result, setResult] = useState<{ scope: string; entries: Record<string, StatsEntry> }>({
    scope: '',
    entries: {},
  });

  useEffect(() => {
    if (!connected) return;
    const request = new AbortController();
    const ids: string[] = JSON.parse(idsKey);
    if (ids.length === 0) return;
    const refresh = async () => {
      const entries: Record<string, StatsEntry> = {};
      try {
        const { start, end } = todayStatsFilters('', day);
        const data = await usageHistoryApi.accountTodayStats(ids, start, end, request.signal);
        for (const id of ids) {
          entries[id] = data[id]
            ? { stats: todayStatsFromUsage(data[id]) }
            : { error: 'Missing account statistics' };
        }
      } catch (error) {
        for (const id of ids) {
          entries[id] = { error: error instanceof Error ? error.message : String(error) };
        }
      }
      if (!request.signal.aborted) setResult({ scope, entries });
    };
    void refresh();
    return () => request.abort();
  }, [connected, day, idsKey, scope, revision, tick]);

  return {
    entries: result.scope === scope && connected ? result.entries : {},
    loading: connected,
  };
}
