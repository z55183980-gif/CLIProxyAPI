package usage

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestPricingTableLongestRuleWins(t *testing.T) {
	table := PricingTable{Default: &PriceCard{InputPerToken: 1}, Rules: []PriceRule{
		{Model: "gpt-*", PriceCard: PriceCard{InputPerToken: 2}},
		{Model: "gpt-5.4", PriceCard: PriceCard{InputPerToken: 3}},
	}}
	card, source, ok := table.Resolve("gpt-5.4")
	if !ok || source != "rule:gpt-5.4" || card.InputPerToken != 3 {
		t.Fatalf("resolve = %+v, %q, %v", card, source, ok)
	}
}

func TestPriceEngineKeepsIndependentSnapshot(t *testing.T) {
	card := PriceCard{InputPerToken: 1, ServiceTier: map[string]float64{"priority": 2},
		ReasoningMultiplier: map[string]float64{"high": 3},
		LongContext:         &LongContextPrice{Threshold: 1, InputMultiplier: 4, OutputMultiplier: 1, Inclusive: true}}
	table := PricingTable{Revision: "r1", Default: &card, Rules: []PriceRule{{Model: "claude-test", PriceCard: card}}}
	engine := NewPriceEngine(table)
	mutate := func(table PricingTable) {
		table.Default.InputPerToken = 99
		table.Default.ServiceTier["priority"] = 99
		table.Default.ReasoningMultiplier["high"] = 99
		table.Default.LongContext.InputMultiplier = 99
		table.Rules[0].Model = "changed"
		table.Rules[0].InputPerToken = 99
		table.Rules[0].ServiceTier["priority"] = 99
		table.Rules[0].ReasoningMultiplier["high"] = 99
		table.Rules[0].LongContext.InputMultiplier = 99
	}
	mutate(table)
	mutate(engine.Table())
	for _, model := range []string{"claude-test", "fallback"} {
		q, err := engine.Quote(model, Detail{TokenBreakdown: NewSubsetTokenBreakdown(1, 0, 0, 0, 0, 1)}, 1, "priority", "high")
		if err != nil || q.TotalUSD != 24 {
			t.Fatalf("quote for %q changed through shared price table: %+v, %v", model, q, err)
		}
		if model == "claude-test" && q.PriceSource != "rule:claude-test" {
			t.Fatalf("source rule changed: %+v", q)
		}
	}
}

func TestPriceEngineRejectsInvalidRateAndAmountOverflow(t *testing.T) {
	detail := Detail{TokenBreakdown: NewSubsetTokenBreakdown(2, 0, 0, 0, 0, 2)}
	for _, tc := range []struct {
		name        string
		price, rate float64
	}{
		{"nan rate", 1, math.NaN()}, {"positive infinite rate", 1, math.Inf(1)},
		{"negative infinite rate", 1, math.Inf(-1)}, {"negative rate", 1, -1},
		{"float overflow", math.MaxFloat64, 2},
		{"ledger overflow", float64(math.MaxInt64) / 1_000_000, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := NewPriceEngine(PricingTable{Default: &PriceCard{InputPerToken: tc.price}})
			if q, err := engine.Quote("model", detail, tc.rate, "", ""); err == nil {
				t.Fatalf("invalid quote accepted: %+v", q)
			}
		})
	}
	engine := NewPriceEngine(PricingTable{Default: &PriceCard{InputPerToken: 1}})
	if q, err := engine.Quote("model", detail, 0, "", ""); err != nil || q.TotalMicros != 0 {
		t.Fatalf("explicit free quote = %+v, %v", q, err)
	}
}

type chargeSinkFunc func(context.Context, Charge) error

func (f chargeSinkFunc) Apply(ctx context.Context, c Charge) error { return f(ctx, c) }

func TestPricingPluginCompletesBillingAfterRequestCancellation(t *testing.T) {
	type contextKey struct{}
	ctx, cancel := context.WithDeadline(context.WithValue(context.Background(), contextKey{}, "trace"), time.Now().Add(-time.Second))
	defer cancel()
	sink := NewMemoryChargeSink()
	var ledgerCtx context.Context
	plugin := &PricingPlugin{
		Engine: NewPriceEngine(PricingTable{Default: &PriceCard{InputPerToken: 1}}),
		Sink: chargeSinkFunc(func(ctx context.Context, c Charge) error {
			ledgerCtx = ctx
			if ctx.Err() != nil || ctx.Value(contextKey{}) != "trace" {
				t.Fatalf("ledger context lost its values or inherited cancellation: %v", ctx.Err())
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 30*time.Second {
				t.Fatalf("ledger context has invalid deadline: %v, %v", deadline, ok)
			}
			return sink.Apply(ctx, c)
		}),
		Totals:  NewBillingTotals(),
		OnError: func(err error) { t.Fatal(err) },
	}
	record := Record{RequestID: "finished", Provider: "claude", Model: "claude-test", AuthID: "account",
		Detail: Detail{TokenBreakdown: NewSubsetTokenBreakdown(1, 0, 0, 0, 0, 1)}}
	plugin.HandleUsage(ctx, record)
	plugin.HandleUsage(ctx, record)
	if len(sink.Charges()) != 1 || plugin.Totals.Snapshot()[0].Requests != 1 {
		t.Fatal("finished request was lost or billed twice")
	}
	if ledgerCtx == nil || ledgerCtx.Err() != context.Canceled {
		t.Fatal("ledger context was not cleaned up after Apply")
	}
}

func TestPricingPluginAcceptsOnlyClaudeProviders(t *testing.T) {
	for _, provider := range []string{"claude", "anthropic", " CLAUDE ", "not-claude", "mock-claude-provider", "openai", ""} {
		t.Run(provider, func(t *testing.T) {
			sink := NewMemoryChargeSink()
			plugin := &PricingPlugin{Engine: NewPriceEngine(PricingTable{Default: &PriceCard{InputPerToken: 1}}), Sink: sink,
				OnError: func(err error) { t.Fatal(err) }}
			plugin.HandleUsage(nil, Record{Provider: provider, Model: "claude-test", Detail: Detail{TokenBreakdown: NewSubsetTokenBreakdown(1, 0, 0, 0, 0, 1)}})
			want := 0
			if provider == "claude" || provider == "anthropic" || provider == " CLAUDE " {
				want = 1
			}
			if got := len(sink.Charges()); got != want {
				t.Fatalf("charges = %d, want %d", got, want)
			}
		})
	}
}

func TestPricingPluginConcurrentRequestsRemainIdempotent(t *testing.T) {
	const requests = 100
	sink := NewMemoryChargeSink()
	plugin := &PricingPlugin{Engine: NewPriceEngine(PricingTable{Default: &PriceCard{InputPerToken: 0.000001}}), Sink: sink,
		Totals: NewBillingTotals(), OnError: func(err error) { t.Error(err) }}
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		for range 3 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				plugin.HandleUsage(context.Background(), Record{RequestID: fmt.Sprintf("r%d", i), AuthID: "single-account", Provider: "claude", Model: "claude-test",
					Detail: Detail{TokenBreakdown: NewSubsetTokenBreakdown(1, 0, 0, 0, 0, 1)}})
			}(i)
		}
	}
	wg.Wait()
	if len(sink.Charges()) != requests {
		t.Fatalf("charges = %d", len(sink.Charges()))
	}
	total := plugin.Totals.Snapshot()[0]
	if total.Requests != requests || total.TotalMicros != requests {
		t.Fatalf("total = %+v", total)
	}
}

func TestMemoryChargeSinkCopiesChargesAndSupportsZeroValue(t *testing.T) {
	var sink MemoryChargeSink
	c := Charge{EventID: "e", Fingerprint: "f", Record: Record{ResponseHeaders: http.Header{"X-Test": {"original"}}, Generate: GenerateFlag(true)}}
	if err := sink.Apply(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	c.Record.ResponseHeaders["X-Test"][0] = "mutated"
	*c.Record.Generate = false
	first := sink.Charges()[0]
	if first.Record.ResponseHeaders.Get("X-Test") != "original" || !*first.Record.Generate {
		t.Fatal("stored record aliases caller data")
	}
	first.Record.ResponseHeaders["X-Test"][0] = "changed snapshot"
	*first.Record.Generate = false
	second := sink.Charges()[0]
	if second.Record.ResponseHeaders.Get("X-Test") != "original" || !*second.Record.Generate {
		t.Fatal("snapshot aliases stored record")
	}
}

func TestRecordFingerprintRemainsCompatibleWithExistingLedger(t *testing.T) {
	r := Record{Model: "claude-test", APIKey: "test-key", Provider: "claude", Detail: Detail{InputTokens: 11, OutputTokens: 7, ReasoningTokens: 3,
		CacheReadTokens: 2, CacheCreationTokens: 1, TotalTokens: 18, TokenBreakdown: NewSubsetTokenBreakdown(11, 2, 1, 7, 3, 18)}}
	const want = "290984b9f789794afa52a77fcfa70cb85060063cef19322e2678c4f856e4a7fd"
	if got := recordFingerprint(r); got != want {
		t.Fatalf("ledger fingerprint changed: %s", got)
	}
}

func TestPriceEngineQuotesMutuallyExclusiveBuckets(t *testing.T) {
	engine := NewPriceEngine(PricingTable{Revision: "r1", Default: &PriceCard{
		InputPerToken: 0.000001, OutputPerToken: 0.000002,
		CacheReadPerToken: 0.0000001, CacheWritePerToken: 0.000003,
	}})
	detail := Detail{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 30, CacheCreationTokens: 10, TotalTokens: 120,
		TokenBreakdown: NewSubsetTokenBreakdown(100, 30, 10, 20, 0, 120)}
	q, err := engine.Quote("model", detail, 2, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if q.InputTokens != 60 || q.OutputTokens != 20 || q.CacheReadTokens != 30 || q.CacheWriteTokens != 10 {
		t.Fatalf("unexpected buckets: %+v", q)
	}
	want := (60*0.000001 + 20*0.000002 + 30*0.0000001 + 10*0.000003) * 2
	if q.TotalUSD != want {
		t.Fatalf("total = %.12f, want %.12f", q.TotalUSD, want)
	}
	if q.TotalMicros != int64(want*1_000_000+0.5) {
		t.Fatalf("micros = %d", q.TotalMicros)
	}
}

func TestPriceEngineLongContextAndTier(t *testing.T) {
	engine := NewPriceEngine(PricingTable{Default: &PriceCard{
		InputPerToken: 1, OutputPerToken: 1,
		ServiceTier: map[string]float64{"priority": 2},
		LongContext: &LongContextPrice{Threshold: 99, InputMultiplier: 3, OutputMultiplier: 4},
	}})
	q, err := engine.Quote("model", Detail{InputTokens: 100, OutputTokens: 10, TotalTokens: 110,
		TokenBreakdown: NewSubsetTokenBreakdown(100, 0, 0, 10, 0, 110)}, 1, "priority", "")
	if err != nil {
		t.Fatal(err)
	}
	if q.TotalUSD != 680 {
		t.Fatalf("total = %v, want 680", q.TotalUSD)
	}
	if !q.LongContextApplied {
		t.Fatal("long context flag is false")
	}
}

func TestMemoryChargeSinkIsIdempotentAndDetectsConflict(t *testing.T) {
	sink := NewMemoryChargeSink()
	c := Charge{EventID: "e1", Fingerprint: "f1"}
	if err := sink.Apply(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if err := sink.Apply(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if err := sink.Apply(context.Background(), Charge{EventID: "e1", Fingerprint: "f2"}); err != ErrChargeConflict {
		t.Fatalf("conflict error = %v", err)
	}
	if len(sink.Charges()) != 1 {
		t.Fatalf("charges = %d", len(sink.Charges()))
	}
}

func TestPricingEngineRejectsUnknownModelWithoutDefault(t *testing.T) {
	engine := NewPriceEngine(PricingTable{Revision: "test", Rules: []PriceRule{{
		Model: "claude-sonnet-*", PriceCard: PriceCard{InputPerToken: 1},
	}}})
	_, err := engine.Quote("claude-unknown-9", Detail{TokenBreakdown: NewSubsetTokenBreakdown(1, 0, 0, 0, 0, 1)}, 1, "", "")
	if err == nil {
		t.Fatal("Quote unexpectedly succeeded for an unpriced model")
	}
}
