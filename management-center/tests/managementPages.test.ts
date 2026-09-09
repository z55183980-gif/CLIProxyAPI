import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { normalizeAccounts } from '../src/services/api/accounts';
import { normalizeProxy, serializeProxy } from '../src/services/api/proxies';

describe('native management pages', () => {
  test('matches billing without displaying secret account identifiers', () => {
    const rows = normalizeAccounts(
      [
        {
          id: 'id-1',
          name: 'claude.json',
          provider: 'claude',
          account: 'secret-api-key',
          disabled: true,
          quota: { signals: { '5h': '0.25', '7d': '75' } },
        },
        { name: 'codex.json', provider: 'codex' },
      ],
      [
        { account: 'secret-api-key', requests: 3, total_tokens: 42, total_micros: 1250000 },
        { account: 'total', total_micros: 9999999 },
      ]
    );
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({
      name: 'claude.json',
      status: 'inactive',
      fiveHour: 25,
      sevenDay: 75,
      requests: 3,
      tokens: 42,
      cost: 1.25,
      rpm: null,
    });
    expect(JSON.stringify(rows)).not.toContain('secret-api-key');
  });
  test('preserves proxy edit fields but excludes observations from writes', () => {
    const proxy = normalizeProxy({
      id: 'p1',
      name: 'test',
      protocol: 'socks5',
      host: 'localhost',
      port: 1080,
      status: 'active',
      expires_at: '2027-01-01T00:00:00Z',
      fallback_mode: 'proxy',
      backup_proxy_id: 'p2',
      expiry_warn_days: 0,
      latency_ms: 0,
      observed_country: 'US',
    });
    expect(proxy.latencyMs).toBe(0);
    const body = serializeProxy(proxy);
    expect(body).toMatchObject({
      backup_proxy_id: 'p2',
      expiry_warn_days: 0,
      expires_at: '2027-01-01T00:00:00Z',
      password: '',
    });
    expect(body).not.toHaveProperty('latency_ms');
    expect(body).not.toHaveProperty('observed_country');
  });
  test('both menus use the shared NavLink renderer and only canonical routes', () => {
    const layout = readFileSync(
      new URL('../src/components/layout/MainLayout.tsx', import.meta.url),
      'utf8'
    );
    const routes = readFileSync(new URL('../src/router/MainRoutes.tsx', import.meta.url), 'utf8');
    for (const path of ['/accounts', '/proxies']) {
      expect(layout).toContain(`path: '${path}'`);
      expect(routes).toContain(`path: '${path}'`);
    }
    expect(layout).toContain('to={item.path}');
    expect(routes).not.toContain("path: '/quota'");
    expect(routes).not.toContain("path: '/account'");
  });
});
