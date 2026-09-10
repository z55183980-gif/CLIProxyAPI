package usagehistory

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
)

func TestNewModelCostsReachAccountWindows(t *testing.T) {
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
	for _, r := range []Record{
		{AccountID: "codex", Provider: "codex", Model: "gpt-6-astra", InputTokens: 1000, OutputTokens: 100, CacheReadTokens: 200, CacheWriteTokens: 20, TotalTokens: 1320},
		{AccountID: "claude", Provider: "claude", Model: "claude-fable-5-1", ReasoningEffort: "max", InputTokens: 1000, OutputTokens: 100, CacheReadTokens: 200, CacheWriteTokens: 20, TotalTokens: 1320},
		{AccountID: "legacy", Provider: "claude", Model: "claude-sonnet-5", InputTokens: 147, OutputTokens: 10, TotalTokens: 157},
	} {
		r.Timestamp = "2026-09-09T16:06:24Z"
		r.AccountingQuality = "complete"
		if errAppend := s.Append(ctx, r); errAppend != nil {
			t.Fatal(errAppend)
		}
	}
	// Reproduce the unpriced Sonnet 5 request from the previous server build.
	if _, errUpdate := s.db.Exec(`UPDATE usage_records SET cost=NULL,payload=json_set(payload,'$.cost',NULL,'$.price',NULL) WHERE account_id='legacy'`); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	stats, err := s.AccountWindowStats(ctx, []string{"codex", "claude", "legacy"}, "2026-09-09T00:00:00Z", "2026-09-10T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	for account, want := range map[string]float64{"codex": .01545, "claude": .0459, "legacy": .000394} {
		got := stats[account]
		if got.Requests != 1 || got.Unpriced != 0 || math.Abs(got.Cost-want) > 1e-10 {
			t.Fatalf("account %s: %+v, want cost %v", account, got, want)
		}
	}
	if stats["legacy"].Estimated != 1 {
		t.Fatalf("missing legacy estimate: %+v", stats["legacy"])
	}
}

func TestBundledClaudeAndCodexPricing(t *testing.T) {
	var catalog map[string]map[string]float64
	if err := json.Unmarshal(modelPricesJSON, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, model, tier, effort string
		input, read, write        int64
		want                      [4]float64
	}{
		{"gpt6 boundary", "gpt-6-astra", "", "", 271998, 1, 1, [4]float64{10, 50, 1, 12.5}},
		{"gpt6 cached context", "gpt-6-astra", "", "", 271999, 1, 1, [4]float64{20, 75, 2, 25}},
		{"gpt6 priority context", "gpt-6-astra", "priority", "", 271999, 1, 1, [4]float64{40, 150, 4, 50}},
		{"gpt6 fast", "gpt-6-astra", "fast", "", 100, 1, 1, [4]float64{20, 100, 2, 25}},
		{"gpt6 ultrafast", "gpt-6-astra", "ultrafast", "", 100, 1, 1, [4]float64{20, 100, 2, 25}},
		{"gpt6 flex context", "gpt-6-astra", "flex", "", 271999, 1, 1, [4]float64{10, 37.5, 1, 12.5}},
		{"gpt6 alias", "openai/gpt-6-ultra", "", "", 100, 1, 1, [4]float64{10, 50, 1, 12.5}},
		{"gpt6 dated effort", "gpt-6-astra-2026-09-01(max)", "", "", 100, 1, 1, [4]float64{10, 50, 1, 12.5}},
		{"sol alias", "gpt-5.6-high", "", "", 100, 1, 1, [4]float64{5, 30, .5, 6.25}},
		{"terra latest", "gpt-5.6-terra", "", "", 100, 1, 1, [4]float64{2, 12, .2, 2.5}},
		{"luna latest", "gpt-5.6-luna", "", "", 100, 1, 1, [4]float64{.2, 1.2, .02, .25}},
		{"sol flex context", "gpt-5.6-sol", "flex", "", 271999, 1, 1, [4]float64{5, 22.5, .5, 6.25}},
		{"fable5", "claude-fable-5", "", "max", 100, 1, 1, [4]float64{10, 50, 1, 12.5}},
		{"fable51", "claude-fable-5-1", "", "high", 100, 1, 1, [4]float64{10, 50, .25, 12.5}},
		{"fable51 max", "anthropic/claude-fable-5.1(max)", "", "", 100, 1, 1, [4]float64{30, 150, .75, 37.5}},
		{"fable51 max suffix", "claude-fable-5-1-max", "", "", 100, 1, 1, [4]float64{30, 150, .75, 37.5}},
		{"fable51 forwarded effort", "claude-fable-5-1(max)", "", "high", 100, 1, 1, [4]float64{10, 50, .25, 12.5}},
		{"fable51 no context surcharge", "claude-fable-5-1", "", "", 900000, 1, 1, [4]float64{10, 50, .25, 12.5}},
		{"sonnet5 official", "claude-sonnet-5", "", "", 100, 1, 1, [4]float64{2, 10, .2, 2.5}},
		{"opus5 fast", "claude-opus-5", "fast", "", 100, 1, 1, [4]float64{10, 50, 1, 12.5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Record{Model: tc.model, ServiceTier: tc.tier, ReasoningEffort: tc.effort, InputTokens: tc.input, OutputTokens: 100, CacheReadTokens: tc.read, CacheWriteTokens: tc.write, AccountingQuality: "complete"}
			p := defaultPrice(r)
			if p == nil {
				t.Fatal("missing price")
			}
			for i, got := range [4]float64{p.Input, p.Output, p.CacheRead, p.CacheWrite} {
				if math.Abs(got-tc.want[i]) > 1e-9 {
					t.Fatalf("price component %d = %v, want %v", i, got, tc.want[i])
				}
			}
			wantCost := (float64(tc.input)*tc.want[0] + 100*tc.want[1] + float64(tc.read)*tc.want[2] + float64(tc.write)*tc.want[3]) / 1e6
			if got := tokenCost(r, p); got == nil || math.Abs(*got-wantCost) > 1e-9 {
				t.Fatalf("cost = %v, want %v", got, wantCost)
			}
		})
	}
	for _, model := range []string{"gpt-6-terra", "gpt-7", "claude-fable-5-10", "claude-fable-6"} {
		if p := defaultPrice(Record{Model: model}); p != nil {
			t.Fatalf("invented price for %s: %+v", model, p)
		}
	}
}

func TestDefaultPricePreservesExactDatedRate(t *testing.T) {
	const model = "pricing-test-model-20260901"
	modelPrices["pricing-test-model"] = map[string]float64{"input_cost_per_token": 1e-6}
	modelPrices[model] = map[string]float64{"input_cost_per_token": 2e-6}
	t.Cleanup(func() {
		delete(modelPrices, "pricing-test-model")
		delete(modelPrices, model)
	})
	p := defaultPrice(Record{Model: "openai/" + model + "(high)", InputTokens: 1})
	if p == nil || p.Input != 2 {
		t.Fatalf("dated price lost: %+v", p)
	}
}
