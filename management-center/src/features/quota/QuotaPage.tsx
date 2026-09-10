import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { authFilesApi } from '@/services/api';
import { Button } from '@/components/ui/Button';
import { EmptyState } from '@/components/ui/EmptyState';
import { Select } from '@/components/ui/Select';
import { IconInfo } from '@/components/ui/icons';
import { Skeleton } from '@/components/ui/Skeleton';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { useNow } from '@/hooks/useNow';
import { useRevealGroup } from '@/hooks/motion';
import { useAuthStore, useQuotaStore, useThemeStore } from '@/stores';
import type { AuthFileItem, ResolvedTheme } from '@/types';
import { ProviderTabs } from '@/features/authFiles/components/ProviderTabs';
import { QuotaHeader } from './components/QuotaHeader';
import { QuotaRow } from './components/QuotaRow';
import { AccountSortHeader } from './components/AccountSortHeader';
import {
  nextAccountColumnSort,
  sortAccountColumns,
  type AccountColumnSort,
  type AccountSortField,
} from './columnSort';
import { QuotaTimeline } from './components/QuotaTimeline';
import {
  CARD_ENTRANCE_BUDGET_MS,
  QUOTA_PAGE_SIZE,
  QUOTA_SORT_MODES,
  QUOTA_TAB_ORDER,
  type QuotaSortMode,
  type QuotaTabId,
} from './constants';
import {
  buildTabCounts,
  classifyQuotaFiles,
  filterEntriesByTab,
  paginate,
  sortQuotaEntries,
  type QuotaFileEntry,
} from './logic';
import { nextRecoveryMs } from './resetSchedule';
import { QUOTA_ADAPTERS, getQuotaSetter, type QuotaCardState } from './providers';
import type { QuotaProviderType } from './providers/types';
import { useQuotaActions } from './hooks/useQuotaActions';
import { useQuotaBatchLoader } from './hooks/useQuotaBatchLoader';
import { readQuotaUiState, writeQuotaUiState } from './uiState';
import styles from './QuotaPage.module.scss';
import { todayStatsAccountId } from './todayStats';
import { useAccountTodayStats } from './hooks/useAccountTodayStats';

const TAB_IDS: string[] = ['all', ...QUOTA_TAB_ORDER];
const SKELETON_ROW_COUNT = 6;

const displayNameFor = (name: string) => name;

export function QuotaPage() {
  const { t } = useTranslation();
  const connectionStatus = useAuthStore((state) => state.connectionStatus);
  const resolvedTheme: ResolvedTheme = useThemeStore((state) => state.resolvedTheme);

  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const scope = JSON.stringify([apiBase, managementKey]);
  const scopeRef = useRef(scope);
  useEffect(() => {
    scopeRef.current = scope;
  }, [scope]);
  const loadRevision = useRef(0);
  const [files, setFiles] = useState<AuthFileItem[]>([]);
  const [statsRevision, setStatsRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [tab, setTab] = useState<QuotaTabId>(() => readQuotaUiState()?.tab ?? 'all');
  const [sortMode, setSortMode] = useState<QuotaSortMode>(
    () => readQuotaUiState()?.sortMode ?? 'default'
  );
  const [page, setPage] = useState(1);
  const [columnSort, setColumnSort] = useState<AccountColumnSort>(null);

  const revealRef = useRevealGroup<HTMLDivElement>();

  const disableControls = connectionStatus !== 'connected';

  const loadFiles = useCallback(async () => {
    const revision = ++loadRevision.current;
    const requestScope = scope;
    setLoading(true);
    setError('');
    try {
      const data = await authFilesApi.list();
      if (revision !== loadRevision.current || requestScope !== scopeRef.current) return;
      setFiles(data?.files || []);
      setStatsRevision((current) => current + 1);
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : t('notification.refresh_failed');
      if (revision === loadRevision.current && requestScope === scopeRef.current) setError(message);
    } finally {
      if (revision === loadRevision.current && requestScope === scopeRef.current) setLoading(false);
    }
  }, [t, scope]);

  useEffect(() => {
    if (disableControls) return;
    let active = true;
    let pending = false;
    const timer = window.setInterval(async () => {
      if (pending || document.hidden) return;
      pending = true;
      try {
        const data = await authFilesApi.list();
        if (active && scope === scopeRef.current) {
          const byName = new Map(data.files.map((file) => [file.name, file]));
          setFiles((current) =>
            current.map((file) => ({
              ...file,
              currentConcurrency: byName.get(file.name)?.currentConcurrency,
            }))
          );
        }
      } catch {
        /* Retain the last observation on transient failures. */
      } finally {
        pending = false;
      }
    }, 5000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [disableControls, scope]);

  const saveNumber = async (
    file: AuthFileItem,
    field: 'concurrency' | 'priority' | 'weight',
    value: number
  ) => {
    const requestScope = scope;
    await authFilesApi.patchFields(file.name, { [field]: value });
    if (requestScope !== scopeRef.current) return;
    setFiles((current) =>
      current.map((item) => (item.name === file.name ? { ...item, [field]: value } : item))
    );
  };

  useHeaderRefresh(loadFiles);

  useEffect(() => {
    void loadFiles();
  }, [loadFiles]);

  const antigravityQuota = useQuotaStore((state) => state.antigravityQuota);
  const claudeQuota = useQuotaStore((state) => state.claudeQuota);
  const codexQuota = useQuotaStore((state) => state.codexQuota);
  const kimiQuota = useQuotaStore((state) => state.kimiQuota);
  const xaiQuota = useQuotaStore((state) => state.xaiQuota);

  const quotaByType = useMemo<Record<QuotaProviderType, Record<string, QuotaCardState>>>(
    () =>
      ({
        antigravity: antigravityQuota,
        claude: claudeQuota,
        codex: codexQuota,
        kimi: kimiQuota,
        xai: xaiQuota,
      }) as unknown as Record<QuotaProviderType, Record<string, QuotaCardState>>,
    [antigravityQuota, claudeQuota, codexQuota, kimiQuota, xaiQuota]
  );

  const getQuota = useCallback(
    (entry: QuotaFileEntry): QuotaCardState | undefined => quotaByType[entry.type][entry.file.name],
    [quotaByType]
  );

  const tick = useNow(sortMode !== 'default');
  const sortNow = sortMode === 'default' ? 0 : tick;

  const entries = useMemo(() => classifyQuotaFiles(files), [files]);
  const tabCounts = useMemo(() => buildTabCounts(entries), [entries]);
  const filteredEntries = useMemo(() => filterEntriesByTab(entries, tab), [entries, tab]);

  const resolveNextRecovery = useCallback(
    (entry: QuotaFileEntry) => nextRecoveryMs(entry.type, getQuota(entry), sortNow),
    [getQuota, sortNow]
  );

  const sortedEntries = useMemo(
    () =>
      columnSort
        ? sortAccountColumns(filteredEntries, columnSort)
        : sortQuotaEntries(filteredEntries, sortMode, resolveNextRecovery),
    [filteredEntries, sortMode, resolveNextRecovery, columnSort]
  );

  const { pageItems, currentPage, totalPages } = useMemo(
    () => paginate(sortedEntries, page, QUOTA_PAGE_SIZE),
    [sortedEntries, page]
  );

  const todayStats = useAccountTodayStats(
    pageItems
      .map((entry) => todayStatsAccountId(entry.file))
      .filter((id): id is string => id !== null),
    statsRevision
  );

  const handleTabChange = useCallback((next: string) => {
    setTab(next as QuotaTabId);
    setPage(1);
    writeQuotaUiState({ tab: next as QuotaTabId });
  }, []);

  const handleSortModeChange = useCallback((next: string) => {
    setColumnSort(null);
    setSortMode(next as QuotaSortMode);
    setPage(1);
    writeQuotaUiState({ sortMode: next as QuotaSortMode });
  }, []);

  const handleColumnSort = useCallback((field: AccountSortField) => {
    setColumnSort((current) => nextAccountColumnSort(current, field));
    setSortMode('default');
    writeQuotaUiState({ sortMode: 'default' });
    setPage(1);
  }, []);

  const sortOptions = useMemo(
    () =>
      QUOTA_SORT_MODES.map((mode) => ({ value: mode, label: t(`quota_management.sort_${mode}`) })),
    [t]
  );

  const { loadedCount, attentionCount } = useMemo(() => {
    let loaded = 0;
    let attention = 0;
    entries.forEach((entry) => {
      const status = quotaByType[entry.type][entry.file.name]?.status;
      if (status === 'success') loaded += 1;
      else if (status === 'error') attention += 1;
    });
    return { loadedCount: loaded, attentionCount: attention };
  }, [entries, quotaByType]);

  useEffect(() => {
    if (loading) return;
    const survivorsByType = new Map<QuotaProviderType, Set<string>>(
      QUOTA_TAB_ORDER.map((type) => [type, new Set<string>()])
    );
    entries.forEach((entry) => survivorsByType.get(entry.type)?.add(entry.file.name));

    QUOTA_TAB_ORDER.forEach((type) => {
      const survivors = survivorsByType.get(type) ?? new Set<string>();
      const setQuota = getQuotaSetter(QUOTA_ADAPTERS[type]);
      setQuota((prev) => {
        const staleKeys = Object.keys(prev).filter((name) => !survivors.has(name));
        if (staleKeys.length === 0) return prev;
        const next = { ...prev };
        staleKeys.forEach((name) => delete next[name]);
        return next;
      });
    });
  }, [entries, loading]);

  const { batchLoading, loadQuota } = useQuotaBatchLoader();
  const { resettingQuotaName, refreshQuota, resetQuota } = useQuotaActions(disableControls);

  const pendingRefreshRef = useRef(false);
  const prevLoadingRef = useRef(loading);

  const handleRefreshAll = useCallback(() => {
    if (disableControls) return;
    pendingRefreshRef.current = true;
    void loadFiles();
  }, [disableControls, loadFiles]);

  useEffect(() => {
    const wasLoading = prevLoadingRef.current;
    prevLoadingRef.current = loading;

    if (!pendingRefreshRef.current) return;
    if (loading || !wasLoading) return;

    pendingRefreshRef.current = false;
    void loadQuota(pageItems.filter((entry) => entry.type !== 'codex' && entry.type !== 'claude'));
  }, [loading, loadQuota, pageItems]);

  const canUseActions = !disableControls && !loading;

  const [cardsAnimated, setCardsAnimated] = useState(false);
  const enableCardEntrance = !cardsAnimated && !loading && pageItems.length > 0;
  useEffect(() => {
    if (enableCardEntrance) {
      setCardsAnimated(true);
    }
  }, [enableCardEntrance]);
  const cardEntranceDelay = (index: number): number | null => {
    if (!enableCardEntrance) return null;
    if (pageItems.length <= 1) return 0;
    return Math.round((index / (pageItems.length - 1)) * CARD_ENTRANCE_BUDGET_MS);
  };

  const isEmpty = !loading && filteredEntries.length === 0;

  return (
    <div className={styles.page} ref={revealRef}>
      <QuotaHeader
        totalCount={entries.length}
        loadedCount={loadedCount}
        attentionCount={attentionCount}
        refreshing={loading || batchLoading}
        disableControls={disableControls}
        onRefreshAll={handleRefreshAll}
      />

      <section className={styles.workbench}>
        <div className={styles.tabsRow} data-reveal>
          <ProviderTabs
            types={TAB_IDS}
            counts={tabCounts}
            active={tab}
            resolvedTheme={resolvedTheme}
            onChange={handleTabChange}
          />
          <div className={styles.sort}>
            <Select
              value={sortMode}
              options={sortOptions}
              onChange={handleSortModeChange}
              ariaLabel={t('quota_management.sort_label')}
              size="sm"
            />
          </div>
        </div>

        {error && (
          <div className={styles.errorBanner} role="alert">
            {error}
          </div>
        )}

        {loading ? (
          <div className={styles.loadingRows} aria-hidden="true">
            {Array.from({ length: SKELETON_ROW_COUNT }, (_, index) => (
              <Skeleton key={index} height={80} rounded={0} />
            ))}
          </div>
        ) : isEmpty ? (
          <EmptyState
            title={
              tab === 'all'
                ? t('quota_management.empty_title')
                : t(`${QUOTA_ADAPTERS[tab].i18nPrefix}.empty_title`)
            }
            description={
              tab === 'all'
                ? t('quota_management.empty_desc')
                : t(`${QUOTA_ADAPTERS[tab].i18nPrefix}.empty_desc`)
            }
            action={
              tab === 'all' ? undefined : (
                <Button variant="secondary" size="sm" onClick={() => handleTabChange('all')}>
                  {t('auth_files.filter_all')}
                </Button>
              )
            }
          />
        ) : (
          <div
            className={styles.tableScroll}
            role="region"
            aria-label={t('quota_management.title')}
            tabIndex={0}
          >
            <table className={styles.table} aria-label={t('quota_management.title')}>
              <thead>
                <tr>
                  <th scope="col" className={styles.accountColumn}>
                    {t('auth_files.column_account')}
                  </th>
                  <th scope="col" className={styles.providerColumn}>
                    {t('auth_files.column_provider')}
                  </th>
                  <th scope="col" className={styles.statusColumn}>
                    {t('auth_files.column_status')}
                  </th>
                  <th scope="col" className={styles.todayColumn}>
                    {t('quota_management.today_title')}
                  </th>
                  <th scope="col" className={styles.quotaColumn}>
                    <span className={styles.usageHeading}>
                      {t('quota_management.window_title')}
                      <span
                        tabIndex={0}
                        title={t('quota_management.window_hint')}
                        aria-label={t('quota_management.window_hint')}
                      >
                        <IconInfo size={14} />
                      </span>
                    </span>
                  </th>
                  <AccountSortHeader
                    field="concurrency"
                    label={t('quota_management.capacity_title')}
                    className={styles.capacityColumn}
                    sort={columnSort}
                    onSort={handleColumnSort}
                  />
                  <AccountSortHeader
                    field="priority"
                    label={t('quota_management.priority_title')}
                    className={styles.priorityColumn}
                    sort={columnSort}
                    onSort={handleColumnSort}
                  />
                  <AccountSortHeader
                    field="weight"
                    label={t('quota_management.weight_title')}
                    className={styles.weightColumn}
                    sort={columnSort}
                    onSort={handleColumnSort}
                  />
                </tr>
              </thead>
              <tbody>
                {pageItems.map((entry, index) => (
                  <QuotaRow
                    key={`${scope}:${entry.type}:${entry.file.name}`}
                    entry={entry}
                    canEdit={canUseActions}
                    onSaveNumber={(field, value) => saveNumber(entry.file, field, value)}
                    quota={getQuota(entry)}
                    todayStats={todayStats.entries[todayStatsAccountId(entry.file) ?? '']?.stats}
                    todayStatsError={
                      todayStats.entries[todayStatsAccountId(entry.file) ?? '']?.error
                    }
                    todayStatsLoading={
                      todayStats.loading &&
                      todayStatsAccountId(entry.file) !== null &&
                      !todayStats.entries[todayStatsAccountId(entry.file) ?? '']
                    }
                    resolvedTheme={resolvedTheme}
                    canRefresh={canUseActions && !entry.file.disabled}
                    resetting={resettingQuotaName === entry.file.name}
                    entranceDelayMs={cardEntranceDelay(index)}
                    onRefresh={() => void refreshQuota(entry.file, QUOTA_ADAPTERS[entry.type])}
                    usageRevision={statsRevision}
                    onReset={() => resetQuota(entry.file, QUOTA_ADAPTERS[entry.type])}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}

        {!loading && filteredEntries.length > 0 && (
          <div className={styles.pagination}>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setPage(Math.max(1, currentPage - 1))}
              disabled={currentPage <= 1}
            >
              {t('auth_files.pagination_prev')}
            </Button>
            <div className={styles.pageInfo}>
              {t('auth_files.pagination_info', {
                current: currentPage,
                total: totalPages,
                count: filteredEntries.length,
              })}
            </div>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setPage(Math.min(totalPages, currentPage + 1))}
              disabled={currentPage >= totalPages}
            >
              {t('auth_files.pagination_next')}
            </Button>
          </div>
        )}

        <QuotaTimeline
          entries={pageItems}
          quotaFor={getQuota}
          displayNameFor={displayNameFor}
          resolvedTheme={resolvedTheme}
        />
      </section>
    </div>
  );
}
