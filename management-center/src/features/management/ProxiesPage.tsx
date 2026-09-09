import { useRef, useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { proxiesApi, type ProxyAccount } from '@/services/api/proxies';
import { useManagementData } from './useManagementData';
import styles from './Management.module.scss';

const emptyProxy = (): ProxyAccount => ({
  id: '',
  name: '',
  protocol: 'http',
  host: '',
  port: 8080,
  username: '',
  password: '',
  status: 'active',
  expiresAt: '',
  fallbackMode: 'none',
  backupProxyId: '',
  expiryWarnDays: 7,
  latencyMs: null,
  latencyStatus: '',
  location: '',
  accountCount: 0,
});

export function ProxiesPage() {
  const { t } = useTranslation();
  const { data, loading, error, refresh } = useManagementData<ProxyAccount[]>(proxiesApi.list, []);
  const [search, setSearch] = useState('');
  const [protocol, setProtocol] = useState('all');
  const [status, setStatus] = useState('all');
  const [selected, setSelected] = useState<string[]>([]);
  const [editor, setEditor] = useState<ProxyAccount | null>(null);
  const [days, setDays] = useState('');
  const [importOpen, setImportOpen] = useState(false);
  const [importText, setImportText] = useState('');
  const [deleteIds, setDeleteIds] = useState<string[]>([]);
  const [accounts, setAccounts] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [failed, setFailed] = useState(false);
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const label = (key: string) => t(`management.${key}`);
  const effectiveStatus = (p: ProxyAccount) =>
    p.expiresAt && Date.parse(p.expiresAt) <= Date.now() ? 'expired' : p.status;
  const rows = data.filter(
    (p) =>
      (protocol === 'all' || p.protocol === protocol) &&
      (status === 'all' || effectiveStatus(p) === status) &&
      [p.name, p.host, p.port, p.location]
        .join(' ')
        .toLowerCase()
        .includes(search.trim().toLowerCase())
  );
  const ids = selected.filter((id) => data.some((p) => p.id === id));
  const allSelected = rows.length > 0 && rows.every((p) => ids.includes(p.id));
  async function run(action: () => Promise<unknown>, done?: () => void) {
    if (busy) return;
    setBusy(true);
    setMessage('');
    setFailed(false);
    try {
      await action();
      if (!mounted.current) return;
      setMessage(label('success'));
      done?.();
    } catch (err) {
      if (mounted.current) {
        setFailed(true);
        setMessage(err instanceof Error ? err.message : String(err));
      }
    } finally {
      if (mounted.current) {
        setBusy(false);
        await refresh();
      }
    }
  }
  async function testAll() {
    const results = await Promise.allSettled(
      (ids.length ? ids : rows.map((p) => p.id)).map(proxiesApi.test)
    );
    const errors = results.filter((r) => r.status === 'rejected');
    if (errors.length)
      throw new Error(label('testFailed') + ` (${errors.length}/${results.length})`);
  }
  const edit = (p: ProxyAccount) => {
    setDays('');
    setEditor({ ...p, status: p.status === 'inactive' ? 'inactive' : 'active' });
  };
  const field = (key: keyof ProxyAccount, value: string | number) =>
    setEditor((p) => (p ? { ...p, [key]: value } : p));
  const select = (key: 'protocol' | 'status' | 'fallbackMode', options: string[]) => (
    <label>
      {label(key)}
      <select
        className="input"
        value={String(editor?.[key])}
        onChange={(e) => field(key, e.target.value)}
      >
        {options.map((v) => (
          <option key={v} value={v}>
            {key === 'protocol' ? v : label(v)}
          </option>
        ))}
      </select>
    </label>
  );

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div>
          <h1>{t('nav.proxies')}</h1>
          <p>{t('nav_meta.proxies')}</p>
        </div>
        <Button onClick={() => edit(emptyProxy())} disabled={busy}>
          {label('addProxy')}
        </Button>
      </header>
      <div className={styles.toolbar}>
        <Input
          aria-label={label('search')}
          placeholder={label('search')}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          className="input"
          aria-label={label('protocol')}
          value={protocol}
          onChange={(e) => setProtocol(e.target.value)}
        >
          {['all', 'http', 'https', 'socks5', 'socks5h'].map((v) => (
            <option key={v} value={v}>
              {v === 'all' ? label('allProtocols') : v}
            </option>
          ))}
        </select>
        <select
          className="input"
          aria-label={label('status')}
          value={status}
          onChange={(e) => setStatus(e.target.value)}
        >
          {['all', 'active', 'inactive', 'expired'].map((v) => (
            <option key={v} value={v}>
              {label(v)}
            </option>
          ))}
        </select>
        <Button variant="secondary" loading={loading} onClick={() => void refresh()}>
          {label('refresh')}
        </Button>
        <Button
          variant="secondary"
          disabled={busy || !rows.length}
          onClick={() => void run(testAll)}
        >
          {label('test')}
        </Button>
        <Button variant="secondary" disabled={busy} onClick={() => setImportOpen(true)}>
          {label('import')}
        </Button>
        <Button
          variant="secondary"
          disabled={busy || !ids.length}
          onClick={() => void run(() => proxiesApi.batch(ids, 'active'))}
        >
          {label('enable')}
        </Button>
        <Button
          variant="secondary"
          disabled={busy || !ids.length}
          onClick={() => void run(() => proxiesApi.batch(ids, 'inactive'))}
        >
          {label('disable')}
        </Button>
        <Button variant="danger" disabled={busy || !ids.length} onClick={() => setDeleteIds(ids)}>
          {label('deleteSelected')}
        </Button>
      </div>
      {error && (
        <p role="alert" className={styles.error}>
          {error}
        </p>
      )}
      {message && (
        <p role={failed ? 'alert' : 'status'} className={failed ? styles.error : styles.muted}>
          {message}
        </p>
      )}
      <div className={`card ${styles.table}`}>
        <table>
          <thead>
            <tr>
              <th>
                <input
                  type="checkbox"
                  aria-label={label('selectAll')}
                  checked={allSelected}
                  onChange={(e) =>
                    setSelected(
                      e.target.checked
                        ? [...new Set([...ids, ...rows.map((p) => p.id)])]
                        : ids.filter((id) => !rows.some((p) => p.id === id))
                    )
                  }
                />
              </th>
              {[
                'name',
                'endpoint',
                'location',
                'accounts',
                'latency',
                'expiresAt',
                'status',
                'actions',
              ].map((key) => (
                <th key={key}>{label(key)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {!rows.length && (
              <tr>
                <td colSpan={9}>{label(loading ? 'loading' : 'empty')}</td>
              </tr>
            )}
            {rows.map((p) => (
              <tr key={p.id}>
                <td>
                  <input
                    type="checkbox"
                    aria-label={`${label('select')} ${p.name}`}
                    checked={ids.includes(p.id)}
                    onChange={(e) =>
                      setSelected(
                        e.target.checked ? [...ids, p.id] : ids.filter((id) => id !== p.id)
                      )
                    }
                  />
                </td>
                <td>
                  <strong>{p.name}</strong>
                  <div className={styles.muted}>
                    {label(p.fallbackMode)} {p.backupProxyId}
                  </div>
                </td>
                <td>
                  {p.protocol}://{p.host}:{p.port}
                </td>
                <td>{p.location || '—'}</td>
                <td>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={busy}
                    onClick={() =>
                      void run(async () => {
                        const names = await proxiesApi.boundAccounts(p.id);
                        if (mounted.current) setAccounts(names);
                      })
                    }
                  >
                    {p.accountCount}
                  </Button>
                </td>
                <td>
                  {p.latencyStatus === 'ok'
                    ? `${p.latencyMs ?? 0} ms`
                    : p.latencyStatus
                      ? label('testFailed')
                      : '—'}
                </td>
                <td>{p.expiresAt ? new Date(p.expiresAt).toLocaleString() : label('never')}</td>
                <td>{label(effectiveStatus(p))}</td>
                <td>
                  <div className={styles.toolbar}>
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={busy}
                      onClick={() => void run(() => proxiesApi.test(p.id))}
                    >
                      {label('test')}
                    </Button>
                    <Button variant="ghost" size="sm" disabled={busy} onClick={() => edit(p)}>
                      {label('edit')}
                    </Button>
                    <Button
                      variant="danger"
                      size="sm"
                      disabled={busy}
                      onClick={() => setDeleteIds([p.id])}
                    >
                      {label('delete')}
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Modal
        open={Boolean(editor)}
        title={label(editor?.id ? 'edit' : 'addProxy')}
        onClose={() => setEditor(null)}
        closeDisabled={busy}
        width={680}
      >
        {editor && (
          <form
            className={styles.form}
            onSubmit={(e) => {
              e.preventDefault();
              const payload = { ...editor };
              if (days)
                payload.expiresAt = new Date(Date.now() + Number(days) * 86400000).toISOString();
              void run(
                () => proxiesApi.save(payload),
                () => setEditor(null)
              );
            }}
          >
            <Input
              label={label('name')}
              required
              value={editor.name}
              onChange={(e) => field('name', e.target.value)}
            />
            {select('protocol', ['http', 'https', 'socks5', 'socks5h'])}
            <Input
              label={label('host')}
              required
              value={editor.host}
              onChange={(e) => field('host', e.target.value)}
            />
            <Input
              label={label('port')}
              type="number"
              min={1}
              max={65535}
              required
              value={editor.port}
              onChange={(e) => field('port', Number(e.target.value))}
            />
            <Input
              label={label('username')}
              autoComplete="off"
              value={editor.username}
              onChange={(e) => field('username', e.target.value)}
            />
            <Input
              label={label('password')}
              type="password"
              autoComplete="new-password"
              value={editor.password || ''}
              onChange={(e) => field('password', e.target.value)}
            />
            {select('status', ['active', 'inactive'])}
            {select('fallbackMode', ['none', 'direct', 'proxy'])}
            <label>
              {label('backupProxyId')}
              <select
                className="input"
                value={editor.backupProxyId}
                required={editor.fallbackMode === 'proxy'}
                disabled={editor.fallbackMode !== 'proxy'}
                onChange={(e) => field('backupProxyId', e.target.value)}
              >
                <option value="">—</option>
                {data
                  .filter((p) => p.id !== editor.id)
                  .map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
              </select>
            </label>
            <Input
              label={label('expiryWarnDays')}
              type="number"
              min={0}
              value={editor.expiryWarnDays}
              onChange={(e) => field('expiryWarnDays', Number(e.target.value))}
            />
            <Input
              label={label('expiresAt')}
              type="date"
              value={editor.expiresAt.slice(0, 10)}
              onChange={(e) => {
                setDays('');
                field(
                  'expiresAt',
                  e.target.value ? new Date(e.target.value + 'T23:59:59').toISOString() : ''
                );
              }}
            />
            <Input
              label={label('days')}
              type="number"
              min={1}
              value={days}
              onChange={(e) => setDays(e.target.value)}
            />
            <Button className={styles.wide} type="submit" loading={busy}>
              {label('save')}
            </Button>
            {failed && message && (
              <p role="alert" className={styles.error}>
                {message}
              </p>
            )}
          </form>
        )}
      </Modal>
      <Modal
        open={importOpen}
        title={label('import')}
        onClose={() => setImportOpen(false)}
        closeDisabled={busy}
      >
        <label>
          {label('importHint')}
          <textarea
            className="input"
            rows={10}
            value={importText}
            onChange={(e) => setImportText(e.target.value)}
          />
        </label>
        {failed && message && (
          <p role="alert" className={styles.error}>
            {message}
          </p>
        )}
        <Button
          loading={busy}
          onClick={() =>
            void run(
              async () => {
                const items: unknown = JSON.parse(importText);
                if (!Array.isArray(items) || !items.length || items.length > 100)
                  throw new Error(label('batchLimit'));
                await proxiesApi.import(items);
              },
              () => {
                setImportOpen(false);
                setImportText('');
              }
            )
          }
        >
          {label('import')}
        </Button>
      </Modal>
      <Modal
        open={deleteIds.length > 0}
        title={label('delete')}
        onClose={() => setDeleteIds([])}
        closeDisabled={busy}
        footer={
          <Button
            variant="danger"
            loading={busy}
            onClick={() =>
              void run(
                () =>
                  deleteIds.length === 1
                    ? proxiesApi.remove(deleteIds[0])
                    : proxiesApi.batch(deleteIds),
                () => {
                  setDeleteIds([]);
                  setSelected([]);
                }
              )
            }
          >
            {label('delete')}
          </Button>
        }
      >
        <p>{t('management.confirmDelete', { count: deleteIds.length })}</p>
        {failed && message && (
          <p role="alert" className={styles.error}>
            {message}
          </p>
        )}
      </Modal>
      <Modal open={accounts !== null} title={label('accounts')} onClose={() => setAccounts(null)}>
        <ul>
          {accounts?.map((name) => (
            <li key={name}>{name}</li>
          ))}
        </ul>
        {!accounts?.length && label('empty')}
      </Modal>
    </div>
  );
}
