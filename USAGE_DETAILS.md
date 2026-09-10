# Usage details

The native `/usage` page appears directly below Dashboard. It implements the
applicable usage-log workflow from the local sub2api reference in this project's
React frontend and Go backend. It does not import sub2api users, groups, balances,
subscriptions, or billing deductions.

## Available features

- Persistent upstream request-attempt records, including failed attempts and retries.
- Account, hashed downstream API key, provider, model, dates, request type,
  HTTP status, and text search filters; success/failure tabs; sorting and pagination.
- Request/token/cost summaries and daily, model, and account distributions.
- Input, output, cache read/write and reasoning tokens, latency and TTFT,
  request/session IDs, endpoint, client IP, and User Agent.
- Column preferences, automatic refresh, request detail dialog, filtered cleanup.
- Cancelable Excel export, up to 100,000 matching rows per export, with a fixed
  upper record ID to exclude new arrivals during export.
- Editable exact-model USD-per-million-token prices. Each request stores the
  price snapshot used for its estimate. No default market prices are assumed.

## Storage and accounting

SQLite stores data at `.usage-history/usage.db` next to the configuration file.
The WAL sidecar files belong to the same database; use a SQLite-aware backup or
stop the server before copying the directory. The directory is Git-ignored and
separate from credential files. Data survives restart and stays until explicitly
cleaned up. Records before this feature was deployed cannot be reconstructed.

The sink attaches to the existing usage dispatcher independently of the optional
transient usage queue. It does not consume `/usage-queue`. Standalone service
mode enables it; upstream Home mode continues to use its remote accounting.

Input tokens shown here are uncached input; cache reads and writes are separate.
Output includes reasoning, which is also displayed as a subset. The existing
canonical upstream token breakdown prevents double counting. Unclassified or
inconsistent token reports are not priced. Unknown prices are null, not zero.
Changing prices affects future requests only. Estimates are not actual bills;
image, per-request, tier-specific, and subscription charges are not inferred.

API keys are stored as SHA-256 identifiers, never as plaintext. Raw prompts,
response headers, credential tokens, and upstream error bodies are not copied
into the history database. Failure records show HTTP status; full error
investigation remains in the existing Logs page. Authentication failures before
an upstream execution are not usage attempts and are not recorded by this sink.

## API

All routes require the normal management authentication:

- `GET /v0/management/usage-history`
- `GET /v0/management/usage-history/options`
- `DELETE /v0/management/usage-history` with `confirm: true` and bounded filters
- `GET /v0/management/usage-prices`
- `PUT /v0/management/usage-prices`

Date ranges use RFC3339 timestamps, inclusive start and exclusive end. Query
filters are shared by totals, distributions, pagination, export and cleanup.
Use the returned `snapshot` on subsequent export pages to exclude later inserts.
Do not concurrently clean up the same range while exporting it.

## Validation

Regression tests cover restart persistence, timezone boundaries, filtering,
non-destructive reads, price snapshots, unknown prices, secret exclusion, safe
cleanup, invalid query parameters, and draining queued records before shutdown.
An isolated local proxy integration check sends successful and failed requests,
checks token/cost results, restarts the process, and verifies the actual browser
page and generated Excel workbook. It never uses production credentials.
