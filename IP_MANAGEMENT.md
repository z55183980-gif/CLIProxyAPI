# Upstream source and IP management

The backend and frontend are full upstream source snapshots with IP management
as the only retained downstream feature. Upstream credential management, quota,
usage statistics, and other standard features remain available.

The interface supports Simplified Chinese and English only. Translation bundles
and menu options for other languages have been removed.

## Source baseline

- Backend: https://github.com/router-for-me/CLIProxyAPI
  at `7fac6b15bcfe5ea55c18c9eaec8e5b7e6457d974`.
- Frontend: https://github.com/router-for-me/Cli-Proxy-API-Management-Center
  at `ed5f1c48e11ba7335f1e8f676f228c280196af85`, vendored in `management-center/`.
- Baselines checked on 2026-09-09.

The custom accounts page, billing extensions, and injected management scripts
have been removed. There are no aliases for their former routes.

## Retained feature

- Native `/proxies` frontend route and sidebar item.
- Proxy creation, editing, deletion, import, batch operations, connectivity
  checks, expiration settings, fallback configuration, and bound credentials.
- Management API routes under `/v0/management/proxy-accounts`.
- Local registry in `<auth-dir>/.proxy-accounts`.
- Bind credentials using the IP selector in the upstream credential details
  editor. Bindings use `proxy_id`; the backend resolves the runtime proxy URL
  and restores it when loading credential files. Unbinding permits manual URLs.
- Proxy passwords are excluded from registry API responses.

## Build and run

From `management-center/`, use Bun 1.3.14:

```powershell
npx --yes bun@1.3.14 install --frozen-lockfile
npx --yes bun@1.3.14 run verify
Copy-Item dist/index.html ../static/management.html -Force
```

From the repository root:

```powershell
go test ./...
go build -o bin/cli-proxy-api.exe ./cmd/server
.\bin\cli-proxy-api.exe --config config.yaml
```

Open `http://127.0.0.1:8317/management.html#/proxies`. Production only needs
the backend process and the generated `static/management.html`; a separate
frontend development server is optional. Both use the same frontend source.
Keep `remote-management.disable-auto-update-panel: true` in local configuration
so the upstream panel updater does not replace the customized build.

## Migration backup and validation

The pre-migration source is retained on local branch
`codex/backup-before-upstream-ip-only-20260909-153902`. Private local config and
auth data were copied into `.git/ip-only-backup-20260909-153902` before migration.
These backups, local credentials, executables, and generated HTML are not source
files to commit.

Frontend verification passed 443 tests, ESLint, TypeScript, and the single-file
production build. Backend IP registry, management handlers, file synthesis, and
SDK tests passed; the server builds successfully. The complete backend suite
has one failure in unchanged upstream test
`TestXAIExecutorExecuteImagesUsesImagesEndpointAndPublishesUsage`: its positive
TTFT assertion observes `0s` on this Windows environment, also on focused rerun.
The upstream executor source and tests were left unchanged.

Browser checks with isolated fixture data verified the desktop and mobile IP
route, restored quota link, and absence of the removed accounts route, without
JavaScript errors. The real backend login page and both local ports were also
checked; fixture UI checks do not validate a user's saved management key.
