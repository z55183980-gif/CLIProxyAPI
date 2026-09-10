package usagehistory

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestAccountWindowStatsUsesCalendarBoundariesAndPriceSnapshots(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	appendRecord := func(r Record) {
		t.Helper()
		if err := s.Append(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	base := Record{Timestamp: "2026-09-09T00:00:00+08:00", AccountID: "claude-account", Model: "claude-sonnet-4-5", InputTokens: 1000000, OutputTokens: 1000000, CacheReadTokens: 1000000, CacheWriteTokens: 1000000, TotalTokens: 4000000, AccountingQuality: "complete"}
	appendRecord(base)
	base.Timestamp = "2026-09-08T23:59:59.999999999+08:00"
	appendRecord(base)
	base.Timestamp = "2026-09-10T00:00:00+08:00"
	appendRecord(base)
	base.Timestamp = "2026-09-09T12:00:00+08:00"
	base.AccountID = "other"
	appendRecord(base)
	if err = s.SavePrices(ctx, []Price{{Model: "claude-sonnet-4-5", Input: 50}}); err != nil {
		t.Fatal(err)
	}
	base.AccountID = "claude-account"
	base.Failed = true
	base.Model = "unknown-model"
	appendRecord(base)
	stats, err := s.AccountWindowStats(ctx, []string{"claude-account", "empty", "claude-account"}, "2026-09-09T00:00:00+08:00", "2026-09-10T00:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	a := stats["claude-account"]
	if len(stats) != 2 || a.Requests != 2 || a.Tokens != 8000000 || a.Unpriced != 1 || math.Abs(a.Cost-36.6) > 1e-9 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats["empty"] != (AccountStats{}) {
		t.Fatalf("nonzero empty account: %+v", stats["empty"])
	}
	for _, ids := range [][]string{{""}, make([]string, 201)} {
		if _, err := s.AccountWindowStats(ctx, ids, base.Timestamp, "2026-09-10T00:00:00Z"); err == nil {
			t.Fatal("accepted invalid account IDs")
		}
	}
	if _, err := s.AccountWindowStats(ctx, []string{"a"}, "", ""); err == nil {
		t.Fatal("accepted unbounded range")
	}
}

func TestAccountStatsEstimatesLegacyRecordsWithoutRewritingHistory(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	r := Record{Timestamp: "2026-09-09T01:00:00Z", AccountID: "codex", Model: "gpt-5.4", InputTokens: 100, OutputTokens: 50, CacheReadTokens: 100, TotalTokens: 250, AccountingQuality: "complete"}
	if err := s.Append(ctx, r); err != nil {
		t.Fatal(err)
	}
	// Simulate a record written before bundled pricing existed.
	r.Timestamp = "2026-09-09T01:00:00.000000000Z"
	raw, _ := json.Marshal(r)
	if _, err := s.db.Exec("UPDATE usage_records SET cost=NULL,payload=?", string(raw)); err != nil {
		t.Fatal(err)
	}
	stats, err := s.AccountWindowStats(ctx, []string{"codex"}, "2026-09-09T00:00:00Z", "2026-09-10T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	a := stats["codex"]
	if a.Estimated != 1 || a.Unpriced != 0 || math.Abs(a.Cost-0.001025) > 1e-10 {
		t.Fatalf("unexpected estimate: %+v", a)
	}
	history, err := s.Query(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if history.Items[0].Cost != nil || history.Items[0].Price != nil {
		t.Fatal("legacy history was rewritten")
	}
}

func TestProviderTokensReachAccountStatsWithoutDoubleCounting(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	p := Plugin{Store: s}
	when, _ := time.Parse(time.RFC3339, "2026-09-09T02:00:00Z")
	p.HandleUsage(context.Background(), usage.Record{AuthID: "codex", Provider: "codex", Model: "gpt-5.4", RequestedAt: when, Detail: usage.Detail{TokenBreakdown: usage.NewSubsetTokenBreakdown(200, 100, 0, 50, 20, 250)}})
	p.HandleUsage(context.Background(), usage.Record{AuthID: "claude", Provider: "claude", Model: "claude-sonnet-4-5", RequestedAt: when, Detail: usage.Detail{TokenBreakdown: usage.NewIndependentTokenBreakdown(100, 40, 10, 30, 20, 200)}})
	stats, err := s.AccountWindowStats(context.Background(), []string{"codex", "claude"}, "2026-09-09T00:00:00Z", "2026-09-10T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if stats["codex"].Tokens != 250 || stats["claude"].Tokens != 200 || stats["codex"].Requests != 1 || stats["claude"].Requests != 1 || stats["claude"].Unpriced != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if math.Abs(stats["codex"].Cost-0.001025) > 1e-10 || math.Abs(stats["claude"].Cost-0.0010995) > 1e-10 {
		t.Fatalf("unexpected costs: %+v", stats)
	}
}

func TestDefaultPriceMatchingAndTiers(t *testing.T) {
	for _, tc := range []struct {
		model, tier string
		input       int64
		price       float64
	}{
		{"gpt-5.4", "", 100, 2.5},
		{"openai/gpt-5.4(high)", "priority", 100, 5},
		{"gpt-5.4", "flex", 100, 1.25},
		{"gpt-5.4", "", 272001, 5},
		{"gpt-5.4", "priority", 272001, 10},
		{"claude-sonnet-4.5", "", 100, 3},
		{"claude-sonnet-4-5-20260909", "", 100, 3},
		{"claude-sonnet-5", "", 100, 2},
		{"claude-fable-5-1", "", 100, 10},
		{"gpt-6", "", 100, 10},
		{"gpt-6-astra", "priority", 100, 20},
		{"gpt-6-astra", "", 272001, 20},
	} {
		p := defaultPrice(Record{Model: tc.model, ServiceTier: tc.tier, InputTokens: tc.input})
		if p == nil || p.Input != tc.price {
			t.Fatalf("%+v: %+v", tc, p)
		}
	}
	if p := defaultPrice(Record{Model: "claude-fable-5-1", ReasoningEffort: "max"}); p == nil || math.Abs(p.Input-30) > 1e-9 {
		t.Fatalf("fable max multiplier: %+v", p)
	}
	if p := defaultPrice(Record{Model: "claude-fable-5-1", CacheReadTokens: 1}); p == nil || p.CacheRead != 0.25 {
		t.Fatalf("fable cache read: %+v", p)
	}
	if defaultPrice(Record{Model: "gpt-unknown"}) != nil {
		t.Fatal("invented unknown model price")
	}
	if tokenCost(Record{AccountingQuality: "inconsistent", TotalTokens: 100}, &Price{Input: 1}) != nil {
		t.Fatal("priced incomplete usage")
	}
}

func TestAccountWindowStatsAccountBillingSnapshots(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if errClose := s.Close(); errClose != nil {
			t.Error(errClose)
		}
	})
	base, account, multiplier := 10.0, 4.0, 0.5
	for _, r := range []Record{
		{Timestamp: "2026-09-09T01:00:00Z", AccountID: "a", Cost: &base, AccountStatsCost: &account, AccountRateMultiplier: &multiplier},
		{Timestamp: "2026-09-09T02:00:00Z", AccountID: "a", Cost: &base},
	} {
		if errAppend := s.Append(ctx, r); errAppend != nil {
			t.Fatal(errAppend)
		}
	}
	result, err := s.AccountWindowStats(ctx, []string{"a"}, "2026-09-09T00:00:00Z", "2026-09-10T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if got := result["a"]; got.Cost != 12 || got.Requests != 2 {
		t.Fatalf("account billing=%+v", got)
	}
	invalid := -1.0
	if err := s.Append(ctx, Record{Timestamp: "2026-09-09T01:00:00Z", AccountRateMultiplier: &invalid}); err == nil {
		t.Fatal("negative billing multiplier accepted")
	}
}
