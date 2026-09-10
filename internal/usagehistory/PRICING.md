# Account usage pricing

`model_prices.json` is a local snapshot based on sub2api commit
[`98d8691`](https://github.com/Wei-Shaw/sub2api/tree/98d86915becae9fe9491a91ffc6defd5235c8d2b),
checked on 2026-09-10. It combines the upstream LiteLLM catalog with sub2api's explicit
fallback policies. GPT-6 Astra is $10/$50 per MTok, with 2x input and 1.5x output
above 272k context tokens. Claude Fable 5.1 is $10/$50 per MTok, $12.50 for 5m
cache writes, $20 for 1h writes, and $0.25 for cache reads; sub2api's `max` effort
policy applies a 3x billing multiplier. Claude Sonnet 5 is $2/$10 per MTok, with
$2.50/$4 cache writes and $0.20 cache reads. Only numeric cost fields are retained;
this is not a live price feed. Context thresholds and the max-effort multiplier are
also retained. GPT-5.6 Terra/Luna and GPT-5.5 priority rates follow this newer catalog.

Sources:

- [sub2api catalog](https://github.com/Wei-Shaw/sub2api/blob/98d86915becae9fe9491a91ffc6defd5235c8d2b/backend/resources/model-pricing/model_prices_and_context_window.json)
- [sub2api billing fallback and multipliers](https://github.com/Wei-Shaw/sub2api/blob/98d86915becae9fe9491a91ffc6defd5235c8d2b/backend/internal/service/billing_service.go)
- [sub2api GPT-6 pricing fallback](https://github.com/Wei-Shaw/sub2api/blob/98d86915becae9fe9491a91ffc6defd5235c8d2b/backend/internal/service/pricing_service.go)
- [Claude Fable 5.1 official prices](https://platform.claude.com/docs/en/models/fable-5-1/overview)
- [Claude Sonnet 5 official prices](https://platform.claude.com/docs/en/models/sonnet-5/overview)

Sonnet 5 is absent from this sub2api catalog and would use a Sonnet-family fallback;
its explicit entry here uses Anthropic's current price instead. Fable 5.1's 3x `max`
multiplier is a sub2api accounting policy, not a claim about Anthropic's API list price.
Fast uses priority rates; Ultrafast uses sub2api's generic 2x standard-rate multiplier.
Explicit management prices remain final per-token overrides, without automatic tiers
or effort multipliers; the bundled policies apply when no override is configured.

Management API model price overrides take precedence. New records preserve a price
and cost snapshot. Account window statistics can estimate older unpriced records
using the current overrides or bundled snapshot without rewriting history. The
response reports the number of estimated and still-unpriced records separately.

Token costs include uncached input, output (including reasoning), cache reads, and
cache writes exactly once. Supported price entries include priority/flex rates and
long-context thresholds. Claude cache writes retain 5m and 1h token buckets and a
separate 1h price snapshot. Missing TTL details use 5m prices; partial details assign
the unclassified remainder to 5m, and over-reported buckets are capped proportionally
at the authoritative cache-write total. Legacy records cannot recover an unreported
TTL. A custom 1h rate is optional; omitting it preserves a single override for both
durations, while an explicit zero remains a zero rate. Non-token charges (images,
audio, search tools) are not available in this usage schema; these totals describe token
costs, not an upstream invoice. No account multiplier is configured in this project,
so the reference implementation's default multiplier of one applies.
