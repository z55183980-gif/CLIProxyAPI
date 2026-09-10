import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import ExcelJS from 'exceljs';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import { Modal } from '@/components/ui/Modal';
import { useAuthStore } from '@/stores';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { downloadBlob } from '@/utils/download';
import {
  usageHistoryApi,
  type UsageFilters,
  type UsageOptions,
  type UsagePrice,
  type UsageRecord,
  type UsageResult,
} from '@/services/api/usageHistory';
import { exportRow, initialUsageFilters, localDateTime, isValidUsageTimeRange } from './usageUtils';
import styles from './UsagePage.module.scss';

const columns = [
  'time',
  'account',
  'apiKey',
  'model',
  'type',
  'tokens',
  'cost',
  'latency',
  'status',
  'endpoint',
  'ip',
  'userAgent',
] as const;
type Column = (typeof columns)[number];
const initialHidden = (): Column[] => {
  try {
    const value: unknown = JSON.parse(localStorage.getItem('usage-hidden-columns') || 'null');
    if (Array.isArray(value))
      return value.filter((v): v is Column => v !== 'time' && columns.includes(v));
  } catch {
    /* Use defaults when storage is unavailable. */
  }
  return ['endpoint', 'ip', 'userAgent'];
};
export function UsagePage() {
  const { t } = useTranslation();
  const l = (key: string) => t(`usageDetails.${key}`);
  const [filters, setFilters] = useState(initialUsageFilters);
  const [draft, setDraft] = useState(filters);
  const [rangeToday] = useState(initialUsageFilters);
  const [result, setResult] = useState<UsageResult | null>(null);
  const [options, setOptions] = useState<UsageOptions>({
    providers: [],
    models: [],
    accounts: [],
    apiKeys: [],
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [detail, setDetail] = useState<UsageRecord | null>(null);
  const [auto, setAuto] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [customTimeOpen, setCustomTimeOpen] = useState(false);
  const [customTimeDraft, setCustomTimeDraft] = useState({ start: '', end: '' });
  const customTimeValid = isValidUsageTimeRange(customTimeDraft.start, customTimeDraft.end);
  const [mobileExpanded, setMobileExpanded] = useState(false);
  const [metric, setMetric] = useState<'requests' | 'tokens' | 'cost'>('requests');
  const [reload, setReload] = useState(0);
  const [hidden, setHidden] = useState(initialHidden);
  const [showColumns, setShowColumns] = useState(false);
  const [priceOpen, setPriceOpen] = useState(false);
  const [prices, setPrices] = useState<UsagePrice[]>([]);
  const [busy, setBusy] = useState(false);
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [exportProgress, setExportProgress] = useState<number | null>(null);
  const operation = useRef<AbortController | null>(null);
  const apiBase = useAuthStore((s) => s.apiBase);
  const key = useAuthStore((s) => s.managementKey);
  const refresh = useCallback(() => setReload((v) => v + 1), []);
  useHeaderRefresh(refresh);
  useEffect(() => {
    const request = new AbortController();
    setLoading(true);
    setError('');
    Promise.all([
      usageHistoryApi.list(filters, request.signal),
      usageHistoryApi.options(request.signal),
    ])
      .then(([data, opts]) => {
        if (!request.signal.aborted) {
          setResult(data);
          setOptions(opts);
        }
      })
      .catch((err: unknown) => {
        if (!request.signal.aborted) setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!request.signal.aborted) setLoading(false);
      });
    return () => request.abort();
  }, [filters, reload, apiBase, key]);
  useEffect(() => {
    setResult(null);
    setDetail(null);
    setPriceOpen(false);
    setCleanupOpen(false);
    setCustomTimeOpen(false);
    setOptions({ providers: [], models: [], accounts: [], apiKeys: [] });
    setNotice('');
    setExportProgress(null);
    setBusy(false);
    return () => operation.current?.abort();
  }, [apiBase, key]);
  useEffect(() => {
    if (!auto) return;
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible' && !loading) refresh();
    }, 10000);
    return () => clearInterval(timer);
  }, [auto, refresh, loading]);
  const format = (n: number) => new Intl.NumberFormat().format(n);
  const money = (n: number | null) => (n === null ? l('unpriced') : `$${n.toFixed(6)}`);
  const apply = (next: UsageFilters) => {
    setDraft(next);
    setFilters({ ...next, page: 1, snapshot: undefined });
  };
  const preset = (days: number) => {
    const next = initialUsageFilters();
    const start = new Date(next.start);
    start.setDate(start.getDate() - days + 1);
    apply({ ...filters, start: start.toISOString(), end: next.end });
  };
  const isPresetSelected = (days: number) => {
    const start = new Date(rangeToday.start);
    start.setDate(start.getDate() - days + 1);
    return draft.start === start.toISOString() && draft.end === rangeToday.end;
  };
  const field = (name: keyof UsageFilters, value: string) =>
    setDraft((prev) => ({ ...prev, [name]: value }));
  const startOperation = () => {
    operation.current?.abort();
    const request = new AbortController();
    operation.current = request;
    setError('');
    setNotice('');
    return request;
  };
  async function editPrices() {
    const request = startOperation();
    setBusy(true);
    try {
      const list = await usageHistoryApi.prices(request.signal);
      if (!request.signal.aborted) {
        setPrices(list);
        setPriceOpen(true);
      }
    } catch (err) {
      if (!request.signal.aborted) setError(String(err));
    } finally {
      if (!request.signal.aborted) setBusy(false);
    }
  }
  async function savePrices() {
    const request = startOperation();
    setBusy(true);
    try {
      await usageHistoryApi.savePrices(prices, request.signal);
      if (!request.signal.aborted) {
        setPriceOpen(false);
        setNotice(l('saved'));
      }
    } catch (err) {
      if (!request.signal.aborted) setError(String(err));
    } finally {
      if (!request.signal.aborted) setBusy(false);
    }
  }
  async function cleanup() {
    const request = startOperation();
    setBusy(true);
    try {
      const res = await usageHistoryApi.cleanup(
        { ...filters, snapshot: result?.snapshot },
        request.signal
      );
      if (!request.signal.aborted) {
        setCleanupOpen(false);
        setNotice(`${l('deleted')}: ${res.deleted}`);
        apply({ ...filters, page: 1 });
        refresh();
      }
    } catch (err) {
      if (!request.signal.aborted) setError(String(err));
    } finally {
      if (!request.signal.aborted) setBusy(false);
    }
  }
  async function exportExcel() {
    const request = startOperation();
    setExportProgress(0);
    try {
      const workbook = new ExcelJS.Workbook();
      const sheet = workbook.addWorksheet('Usage');
      let page = 1;
      let exported = 0;
      let snapshot: number | undefined;
      while (!request.signal.aborted) {
        const data = await usageHistoryApi.list(
          { ...filters, page, pageSize: 1000, snapshot, sort: 'timestamp', order: 'asc' },
          request.signal
        );
        snapshot = data.snapshot;
        if (request.signal.aborted) return;
        if (data.total > 100000) throw new Error(l('exportLimit'));
        for (const record of data.items) {
          const row = exportRow(record);
          if (!sheet.columns?.length)
            sheet.columns = Object.keys(row).map((k) => ({ header: l(k), key: k, width: 22 }));
          sheet.addRow(row);
        }
        exported += data.items.length;
        setExportProgress(
          data.total ? Math.min(100, Math.round((exported / data.total) * 100)) : 100
        );
        if (!data.items.length || exported >= data.total) break;
        page++;
      }
      if (request.signal.aborted) return;
      sheet.views = [{ state: 'frozen', ySplit: 1 }];
      const buffer = await workbook.xlsx.writeBuffer();
      if (request.signal.aborted) return;
      downloadBlob({
        filename: `usage-${new Date().toISOString().slice(0, 10)}.xlsx`,
        blob: new Blob([buffer as ArrayBuffer], {
          type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
        }),
      });
    } catch (err) {
      if (!request.signal.aborted) setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (!request.signal.aborted) setExportProgress(null);
    }
  }
  const select = (
    name: 'provider' | 'model' | 'accountId' | 'apiKeyId' | 'requestType',
    items: { value: string; label: string }[]
  ) => (
    <div className={styles.filterField}>
      <label htmlFor={`usage-filter-${name}`}>{l(name)}</label>
      <Select
        id={`usage-filter-${name}`}
        value={draft[name]}
        options={[{ value: '', label: l('all') }, ...items]}
        onChange={(value) => field(name, value)}
      />
    </div>
  );
  const cell = (r: UsageRecord, c: Column) => {
    switch (c) {
      case 'time':
        return (
          <Button variant="ghost" size="sm" onClick={() => setDetail(r)}>
            <span>{new Date(r.timestamp).toLocaleString()}</span>
            <small className={styles.detailLink}>{l('viewDetail')}</small>
          </Button>
        );
      case 'account':
        return r.account || '—';
      case 'apiKey':
        return r.apiKeyId || '—';
      case 'model':
        return (
          <>
            {r.model}
            <small>
              {r.provider} · {r.reasoningEffort || '—'}
            </small>
          </>
        );
      case 'type':
        return (
          <>
            {l(r.stream ? 'stream' : 'nonStream')}
            <small>{r.serviceTier}</small>
          </>
        );
      case 'tokens':
        return (
          <>
            {format(r.totalTokens)}
            <small>
              {l('input')} {format(r.inputTokens)} / {l('output')} {format(r.outputTokens)}
            </small>
            <small>
              {l('cacheRead')} {format(r.cacheReadTokens)} / {l('cacheWrite')}{' '}
              {format(r.cacheWriteTokens)}
            </small>
            {(r.cacheWrite5mTokens !== undefined || r.cacheWrite1hTokens !== undefined) && (
              <small>
                {l('cacheWrite5m')} {format(r.cacheWrite5mTokens ?? 0)} / {l('cacheWrite1h')}{' '}
                {format(r.cacheWrite1hTokens ?? 0)}
              </small>
            )}
          </>
        );
      case 'cost':
        return (
          <span className={r.cost === null ? styles.unpriced : undefined}>{money(r.cost)}</span>
        );
      case 'latency':
        return (
          <>
            {format(r.latencyMs)} ms<small>TTFT {format(r.ttftMs)} ms</small>
          </>
        );
      case 'status':
        return (
          <span className={r.failed ? styles.failed : styles.success}>
            {r.statusCode} · {l(r.failed ? 'failed' : 'success')}
          </span>
        );
      case 'endpoint':
        return r.endpoint;
      case 'ip':
        return r.clientIp || '—';
      case 'userAgent':
        return r.userAgent || '—';
    }
  };
  const shown = columns.filter((c) => !hidden.includes(c));
  const stats = result?.stats;
  const activeAdvanced = [
    'provider',
    'accountId',
    'apiKeyId',
    'requestType',
    'statusCode',
    'search',
  ].filter((key) => Boolean(filters[key as keyof UsageFilters])).length;
  const cleanupConditions = [
    [l('status'), l(filters.status || 'all')],
    [l('provider'), filters.provider],
    [l('model'), filters.model],
    [
      l('accountId'),
      options.accounts.find((a) => a.id === filters.accountId)?.name || filters.accountId,
    ],
    [l('apiKeyId'), filters.apiKeyId],
    [
      l('requestType'),
      filters.requestType ? l(filters.requestType === 'stream' ? 'stream' : 'nonStream') : '',
    ],
    [l('statusCode'), filters.statusCode],
    [l('search'), filters.search],
  ].filter(([, value]) => Boolean(value));
  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>{l('title')}</h1>
          <p>{l('subtitle')}</p>
        </div>
        <div className={styles.toolbar}>
          <Button variant="secondary" disabled={busy} onClick={() => void editPrices()}>
            {l('prices')}
          </Button>
          <Button
            variant="secondary"
            disabled={exportProgress !== null || !result?.total}
            onClick={() => void exportExcel()}
          >
            {l('export')}
          </Button>
          <Button loading={loading} onClick={refresh}>
            {l('refresh')}
          </Button>
        </div>
      </header>
      {error && (
        <div role="alert" className={styles.error}>
          {error}
        </div>
      )}
      {notice && <p role="status">{notice}</p>}
      {!!result?.writeErrors && (
        <div role="alert" className={styles.error}>
          {l('writeErrors')}: {result.writeErrors}
        </div>
      )}
      <div className={styles.stats}>
        {[
          [
            'requests',
            format(stats?.requests || 0),
            `${l('failed')}: ${format(stats?.failed || 0)}`,
          ],
          [
            'tokens',
            format(stats?.tokens || 0),
            `${l('cacheRead')}: ${format(stats?.cacheRead || 0)}`,
          ],
          [
            'cost',
            money(
              stats && stats.requests > 0 && stats.unpriced === stats.requests
                ? null
                : stats?.cost || 0
            ),
            `${l('unpriced')}: ${format(stats?.unpriced || 0)}`,
          ],
          [
            'latency',
            `${Math.round(stats?.latency || 0)} ms`,
            `TTFT ${Math.round(stats?.ttft || 0)} ms`,
          ],
        ].map(([name, value, hint]) => (
          <div className={styles.stat} key={name}>
            <span>{l(name)}</span>
            <strong
              className={name === 'cost' && value === l('unpriced') ? styles.unpriced : undefined}
            >
              {value}
            </strong>
            <small>{hint}</small>
          </div>
        ))}
      </div>
      {!!result?.total && (
        <>
          <div className={styles.toolbar} role="group" aria-label={l('metric')}>
            <span className={styles.muted}>{l('chartMetric')}</span>
            {(['requests', 'tokens', 'cost'] as const).map((value) => (
              <Button
                key={value}
                size="sm"
                variant={metric === value ? 'primary' : 'ghost'}
                aria-pressed={metric === value}
                onClick={() => setMetric(value)}
              >
                {l(value)}
              </Button>
            ))}
          </div>
          <div className={styles.charts}>
            {(['trend', 'models', 'accounts'] as const).map((kind) => (
              <section className={styles.panel} key={kind}>
                <h3>
                  {l(kind)} <small className={styles.muted}>· {l(metric)}</small>
                </h3>
                {(result?.[kind] || []).slice(0, 7).map((g) => (
                  <div className={styles.bar} key={g.name}>
                    <span title={g.name}>{g.name || '—'}</span>
                    <progress
                      aria-label={g.name}
                      value={g[metric]}
                      max={Math.max(0.000001, ...(result?.[kind] || []).map((v) => v[metric]))}
                    />
                    <span>{metric === 'cost' ? money(g.cost) : format(g[metric])}</span>
                  </div>
                ))}
                {!result?.[kind].length && <p className={styles.muted}>{l('empty')}</p>}
              </section>
            ))}
          </div>
        </>
      )}
      <section className={styles.panel}>
        <div className={styles.filterPresets}>
          <div className={styles.tabs} role="group" aria-label={l('status')}>
            {['', 'success', 'failed'].map((status) => (
              <Button
                key={status}
                variant={filters.status === status ? 'primary' : 'ghost'}
                aria-pressed={filters.status === status}
                onClick={() => apply({ ...filters, status })}
              >
                {l(status || 'all')}
              </Button>
            ))}
          </div>
          <div
            className={`${styles.toolbar} ${styles.quickRanges}`}
            role="group"
            aria-label={l('dateRange')}
          >
            {[1, 7, 30].map((days) => (
              <Button
                key={days}
                type="button"
                size="sm"
                variant={isPresetSelected(days) ? 'primary' : 'ghost'}
                aria-pressed={isPresetSelected(days)}
                onClick={() => preset(days)}
              >
                {l(days === 1 ? 'today' : days === 7 ? 'last7' : 'last30')}
              </Button>
            ))}
            <Button
              type="button"
              size="sm"
              variant={[1, 7, 30].some(isPresetSelected) ? 'ghost' : 'primary'}
              aria-pressed={![1, 7, 30].some(isPresetSelected)}
              aria-haspopup="dialog"
              title={`${localDateTime(draft.start).replace('T', ' ')} — ${localDateTime(draft.end).replace('T', ' ')}`}
              onClick={() => {
                setCustomTimeDraft({
                  start: localDateTime(draft.start),
                  end: localDateTime(draft.end),
                });
                setCustomTimeOpen(true);
              }}
            >
              {l('customTime')}
            </Button>
          </div>
        </div>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            apply(draft);
          }}
        >
          <div className={styles.primaryFilters}>
            {select(
              'model',
              options.models.map((value) => ({ value, label: value }))
            )}
            <Button
              className={styles.advancedToggle}
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => setAdvancedOpen((v) => !v)}
              aria-expanded={advancedOpen}
            >
              {advancedOpen ? l('hideAdvanced') : l('showAdvanced')}
              {activeAdvanced > 0 && ` (${activeAdvanced})`}
            </Button>
            <div className={`${styles.toolbar} ${styles.filterActions}`}>
              <Button type="submit">{l('apply')}</Button>
              <Button type="button" variant="ghost" onClick={() => apply(initialUsageFilters())}>
                {l('reset')}
              </Button>
              <label>
                <input type="checkbox" checked={auto} onChange={(e) => setAuto(e.target.checked)} />{' '}
                {l('auto')}
              </label>
              <Button type="button" variant="ghost" onClick={() => setShowColumns((v) => !v)}>
                {l('columns')}
              </Button>
              <Button
                type="button"
                variant="danger"
                disabled={!result?.total || !filters.start || !filters.end || busy || loading}
                onClick={() => setCleanupOpen(true)}
              >
                {l('cleanup')}
              </Button>
            </div>
          </div>
          {advancedOpen && (
            <div className={styles.filters}>
              {select(
                'provider',
                options.providers.map((value) => ({ value, label: value }))
              )}
              {select(
                'accountId',
                options.accounts.map((a) => ({ value: a.id, label: a.name || a.id }))
              )}
              {select(
                'apiKeyId',
                options.apiKeys.map((value) => ({ value, label: value }))
              )}
              {select('requestType', [
                { value: 'stream', label: l('stream') },
                { value: 'non_stream', label: l('nonStream') },
              ])}
              <Input
                label={l('statusCode')}
                type="number"
                min={100}
                max={599}
                value={draft.statusCode}
                onChange={(e) => field('statusCode', e.target.value)}
              />
              <Input
                label={l('search')}
                placeholder={l('searchHint')}
                value={draft.search}
                onChange={(e) => field('search', e.target.value)}
              />
            </div>
          )}
        </form>
        {showColumns && (
          <div className={styles.columns}>
            {columns.map((c) => (
              <label key={c}>
                <input
                  type="checkbox"
                  checked={!hidden.includes(c)}
                  disabled={c === 'time'}
                  onChange={() => {
                    const next = hidden.includes(c)
                      ? hidden.filter((v) => v !== c)
                      : [...hidden, c];
                    setHidden(next);
                    try {
                      localStorage.setItem('usage-hidden-columns', JSON.stringify(next));
                    } catch {
                      /* Optional preference. */
                    }
                  }}
                />
                {l(c)}
              </label>
            ))}
          </div>
        )}
        <div className={styles.toolbar}>
          <div className={styles.sortField}>
            <label htmlFor="usage-sort">{l('sort')}</label>
            <Select
              id="usage-sort"
              className={styles.sortSelect}
              fullWidth={false}
              value={filters.sort}
              onChange={(value) => apply({ ...filters, sort: value })}
              options={[
                ['timestamp', 'time'],
                ['model', 'model'],
                ['total_tokens', 'tokens'],
                ['cost', 'cost'],
                ['latency_ms', 'latency'],
                ['ttft_ms', 'ttft'],
              ].map(([value, key]) => ({ value, label: l(key) }))}
            />
          </div>
          <Button
            variant="ghost"
            onClick={() => apply({ ...filters, order: filters.order === 'desc' ? 'asc' : 'desc' })}
          >
            {l(filters.order)}
          </Button>
        </div>
        <div className={styles.mobileControls}>
          <Button
            variant="ghost"
            size="sm"
            aria-pressed={mobileExpanded}
            onClick={() => setMobileExpanded((value) => !value)}
          >
            {l(mobileExpanded ? 'compactColumns' : 'expandColumns')}
          </Button>
          <span className={styles.muted}>{l('scrollTable')}</span>
        </div>
        <div className={`${styles.table} ${mobileExpanded ? styles.expandedTable : ''}`}>
          <table>
            <thead>
              <tr>
                {shown.map((c) => (
                  <th key={c} data-column={c}>
                    {l(c)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {result?.items.map((r) => (
                <tr key={r.id}>
                  {shown.map((c) => (
                    <td key={c} data-column={c}>
                      {cell(r, c)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
          {!result?.items.length && (
            <div className={styles.empty}>{loading ? l('loading') : l('empty')}</div>
          )}
        </div>
        <div className={styles.pagination}>
          <span>
            {l('total')}: {format(result?.total || 0)}
          </span>
          <div className={styles.toolbar}>
            <Select
              className={styles.pageSizeSelect}
              ariaLabel={l('pageSize')}
              fullWidth={false}
              size="sm"
              value={String(filters.pageSize)}
              onChange={(value) => apply({ ...filters, pageSize: Number(value) })}
              options={[25, 50, 100].map((n) => ({ value: String(n), label: String(n) }))}
            />
            <Button
              variant="secondary"
              disabled={loading || filters.page === 1}
              onClick={() => setFilters((f) => ({ ...f, page: f.page - 1 }))}
            >
              {l('previous')}
            </Button>
            <span>
              {filters.page} / {Math.max(1, Math.ceil((result?.total || 0) / filters.pageSize))}
            </span>
            <Button
              variant="secondary"
              disabled={loading || filters.page * filters.pageSize >= (result?.total || 0)}
              onClick={() => setFilters((f) => ({ ...f, page: f.page + 1 }))}
            >
              {l('next')}
            </Button>
          </div>
        </div>
      </section>
      <p className={styles.muted}>{l('dataNote')}</p>
      <Modal
        open={customTimeOpen}
        title={l('customTime')}
        onClose={() => setCustomTimeOpen(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setCustomTimeOpen(false)}>
              {l('cancel')}
            </Button>
            <Button
              disabled={!customTimeValid}
              onClick={() => {
                if (!customTimeValid) return;
                apply({
                  ...draft,
                  start: new Date(customTimeDraft.start).toISOString(),
                  end: new Date(customTimeDraft.end).toISOString(),
                });
                setCustomTimeOpen(false);
              }}
            >
              {l('apply')}
            </Button>
          </>
        }
      >
        <div className={styles.filters}>
          <Input
            label={l('start')}
            type="datetime-local"
            required
            value={customTimeDraft.start}
            onChange={(e) => setCustomTimeDraft((v) => ({ ...v, start: e.target.value }))}
          />
          <Input
            label={l('end')}
            type="datetime-local"
            required
            value={customTimeDraft.end}
            onChange={(e) => setCustomTimeDraft((v) => ({ ...v, end: e.target.value }))}
          />
        </div>
        {!customTimeValid && (
          <p role="status" className={styles.muted}>
            {l('invalidTimeRange')}
          </p>
        )}
      </Modal>
      <Modal open={!!detail} title={l('detail')} onClose={() => setDetail(null)} width={800}>
        {detail && (
          <dl className={styles.detail}>
            {Object.entries(exportRow(detail)).map(([key, value]) => (
              <div key={key}>
                <dt>{l(key)}</dt>
                <dd>{value === '' ? '—' : String(value)}</dd>
              </div>
            ))}
            <div>
              <dt>{l('accountingQuality')}</dt>
              <dd>{detail.accountingQuality}</dd>
            </div>
            <div>
              <dt>{l('priceSnapshot')}</dt>
              <dd>{detail.price ? JSON.stringify(detail.price, null, 2) : l('unpriced')}</dd>
            </div>
          </dl>
        )}
      </Modal>
      <Modal
        open={priceOpen}
        title={l('prices')}
        onClose={() => setPriceOpen(false)}
        width={1000}
        closeDisabled={busy}
        footer={
          <>
            <Button variant="ghost" disabled={busy} onClick={() => setPriceOpen(false)}>
              {l('cancel')}
            </Button>
            <Button loading={busy} onClick={() => void savePrices()}>
              {l('save')}
            </Button>
          </>
        }
      >
        <p className={styles.muted}>{l('priceNote')}</p>
        <div className={`${styles.table} ${styles.prices}`}>
          <table>
            <thead>
              <tr>
                {['model', 'input', 'output', 'cacheRead', 'cacheWrite', 'cacheWrite1h'].map(
                  (k) => (
                    <th key={k}>{l(k)}</th>
                  )
                )}
                <th>{l('remove')}</th>
              </tr>
            </thead>
            <tbody>
              {prices.map((p, index) => (
                <tr key={index}>
                  {(
                    ['model', 'input', 'output', 'cacheRead', 'cacheWrite', 'cacheWrite1h'] as const
                  ).map((k) => (
                    <td key={k}>
                      <input
                        className="input"
                        aria-label={`${l(k)} ${index + 1}`}
                        type={k === 'model' ? 'text' : 'number'}
                        min={0}
                        step="any"
                        value={p[k] ?? ''}
                        onChange={(e) =>
                          setPrices((list) =>
                            list.map((v, i) =>
                              i === index
                                ? {
                                    ...v,
                                    [k]:
                                      k === 'model'
                                        ? e.target.value
                                        : k === 'cacheWrite1h' && e.target.value === ''
                                          ? undefined
                                          : Number(e.target.value),
                                  }
                                : v
                            )
                          )
                        }
                      />
                    </td>
                  ))}
                  <td>
                    <Button
                      variant="ghost"
                      onClick={() => setPrices((list) => list.filter((_, i) => i !== index))}
                    >
                      {l('remove')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <Button
          variant="secondary"
          onClick={() =>
            setPrices((list) => [
              ...list,
              { model: '', input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
            ])
          }
        >
          {l('addPrice')}
        </Button>
      </Modal>
      <Modal
        open={cleanupOpen}
        title={l('cleanup')}
        onClose={() => setCleanupOpen(false)}
        closeDisabled={busy}
        footer={
          <>
            <Button variant="ghost" onClick={() => setCleanupOpen(false)} disabled={busy}>
              {l('cancel')}
            </Button>
            <Button variant="danger" loading={busy} onClick={() => void cleanup()}>
              {l('confirmDelete')}
            </Button>
          </>
        }
      >
        <p>{l('cleanupNote')}</p>
        <dl className={styles.detail}>
          {cleanupConditions.map(([name, value]) => (
            <div key={name}>
              <dt>{name}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
        <p>
          {new Date(filters.start).toLocaleString()} — {new Date(filters.end).toLocaleString()}
        </p>
        <p>
          {l('total')}: {result?.total}
        </p>
      </Modal>
      <Modal
        open={exportProgress !== null}
        title={l('export')}
        onClose={() => {
          operation.current?.abort();
          setExportProgress(null);
        }}
      >
        <progress max={100} value={exportProgress || 0} />
        <p>{exportProgress}%</p>
        <Button
          onClick={() => {
            operation.current?.abort();
            setExportProgress(null);
          }}
        >
          {l('cancel')}
        </Button>
      </Modal>
    </div>
  );
}
