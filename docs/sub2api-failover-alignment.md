# Provider failover reference

The local Claude/Codex generation dispatcher follows the applicable provider
failure policies in Wei-Shaw/sub2api commit
`98d86915becae9fe9491a91ffc6defd5235c8d2b` (2026-09-09, v0.2.4).

| Behavior | Upstream reference |
| --- | --- |
| Claude HTTP retries and pre-output overload | `gateway_forward.go` |
| Claude transport failover and durable network cooldown | `gateway_upstream_transport_error.go` |
| Fable credits-required isolation, shared/Fable quota windows, HTML 403 exemption and explicit 529 policy | `ratelimit_service.go` |
| Codex transient OAuth 429 retry window, Retry-After cap and three-account limit | `openai_account_runtime_block_fastpath.go`, `handler/openai_gateway_handler.go` |
| Request-local capacity backoff | `handler/failover_loop.go`, `openai_gateway_upstream_errors.go` |
| Structured context, access-state and missing-model classification | `openai_gateway_upstream_errors.go` |
| Semantic stream status, credential versus policy errors, capacity error normalization | `openai_gateway_passthrough.go` |
| API-key transient failure streak lifetime | `openai_account_model_transient.go` |
| Deactivated workspace propagation | `openai_team_linked_error.go` |

The OAuth 429 recovery window is two minutes per account, shared by concurrent
requests. It does not set a network timeout. Waiting uses Retry-After when present,
capped at eight seconds and the remaining recovery window. Explicit exhausted
quota signals skip this recovery window. Ordinary Codex stream 429 events ignore
the enclosing HTTP 200 quota snapshot; Spark model quota uses its own window.

Capacity shedding retries the same account before switching, with default waits
of 0.5, 1 and 2 seconds. Pool retry settings retain their existing limits. It does
not penalize account health. Once output has been committed, errors are reported
without replaying the request. Outgoing capacity codes become `server_error`;
classification retains the original upstream error.

Fable `credits_required` and the `7d_oi` window affect the Fable model family.
Explicitly exhausted shared 5-hour or 7-day windows still block the account.
No separate Fable 5-hour quota is invented.

This is an adaptation to CLIProxyAPI's local credential scheduler, not an import
of sub2api's application services. Home distributed scheduling, token counting,
other providers, billing/profit controls, administrative usage thresholds,
Redis account-health scoring and shadow-account provisioning are outside this
dispatcher. Runtime account/model state uses the existing local persistence and
scheduler publication paths. Optional upstream first-output timeouts are not
enabled: this repository forbids new post-connection network timeouts.

Regression coverage is in `sub2api_failover_latest_test.go`,
`sub2api_failover_test.go`, and the real executor HTTP/SSE/WebSocket tests under
`test/`. Retry-window tests use a controllable clock.
