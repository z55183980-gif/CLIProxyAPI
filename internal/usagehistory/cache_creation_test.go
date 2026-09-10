package usagehistory

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestClaudeCacheTTLPricesAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		model, effort     string
		five, hour, input int64
		want              float64
	}{
		{"claude-fable-5-1", "", 30, 70, 0, .001775},
		{"claude-fable-5-1", "max", 30, 70, 0, .005325},
		{"claude-sonnet-5", "", 30, 70, 0, .000355},
		{"claude-opus-5", "", 30, 70, 0, .0008875},
		{"claude-sonnet-4-5", "", 30, 70, 200001, 1.201071},
	} {
		r := Record{Model: tc.model, ReasoningEffort: tc.effort, InputTokens: tc.input, CacheWriteTokens: 100, CacheWrite5mTokens: tc.five, CacheWrite1hTokens: tc.hour, AccountingQuality: "complete"}
		p := defaultPrice(r)
		if p == nil || p.CacheWrite1h == nil {
			t.Fatalf("missing 1h rate for %s", tc.model)
		}
		if cost := tokenCost(r, p); cost == nil || math.Abs(*cost-tc.want) > 1e-9 {
			t.Fatalf("%s/%s: cost %v, want %v", tc.model, tc.effort, cost, tc.want)
		}
	}
	r := Record{CacheWriteTokens: 100, CacheWrite5mTokens: 30, CacheWrite1hTokens: 70, AccountingQuality: "complete"}
	for _, tc := range []struct {
		hour *float64
		want float64
	}{{nil, .0002}, {new(float64), .00006}} {
		p := &Price{CacheWrite: 2, CacheWrite1h: tc.hour}
		if cost := tokenCost(r, p); cost == nil || math.Abs(*cost-tc.want) > 1e-9 {
			t.Fatalf("override cost %v, want %v", cost, tc.want)
		}
	}
}

func TestClaudeCacheTTLReachesHistoryAndAccountCost(t *testing.T) {
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
	d := helps.ParseClaudeUsage([]byte(`{"usage":{"input_tokens":10,"output_tokens":12,"cache_read_input_tokens":20,"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":70}}}`))
	p := Plugin{Store: s}
	p.HandleUsage(ctx, usage.Record{Provider: "claude", Model: "claude-fable-5-1", AuthID: "ttl-account", RequestedAt: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), Detail: d})
	history, err := s.Query(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Items) != 1 {
		t.Fatalf("missing history: %+v", history)
	}
	r := history.Items[0]
	if r.CacheWrite5mTokens != 30 || r.CacheWrite1hTokens != 70 || r.CacheWriteTokens != 100 || r.TotalTokens != 142 || r.Price == nil || r.Price.CacheWrite1h == nil || *r.Price.CacheWrite1h != 20 {
		t.Fatalf("TTL snapshot lost: %+v", r)
	}
	stats, err := s.AccountWindowStats(ctx, []string{"ttl-account"}, "2026-09-10T00:00:00Z", "2026-09-11T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if a := stats["ttl-account"]; a.Unpriced != 0 || math.Abs(a.Cost-.00248) > 1e-9 {
		t.Fatalf("wrong account cost: %+v", a)
	}
	negative := -1.0
	if errSave := s.SavePrices(ctx, []Price{{Model: "invalid", CacheWrite1h: &negative}}); errSave == nil {
		t.Fatal("accepted negative 1h price")
	}
}
