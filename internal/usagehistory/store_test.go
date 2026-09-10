package usagehistory

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestPersistFilterPriceSnapshotAndCleanup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	prices := []Price{{Model: "model-a", Input: 2, Output: 4, CacheRead: 0.5, CacheWrite: 3}}
	if err = s.SavePrices(ctx, prices); err != nil {
		t.Fatal(err)
	}
	base := Record{Timestamp: "2026-09-09T01:00:00Z", Model: "model-a", Provider: "claude", AccountID: "a", Account: "Account A", APIKeyID: "hashed-key", InputTokens: 1000000, OutputTokens: 1000000, CacheReadTokens: 1000000, CacheWriteTokens: 1000000, TotalTokens: 4000000, AccountingQuality: "complete", StatusCode: 200}
	if err = s.Append(ctx, base); err != nil {
		t.Fatal(err)
	}
	prices[0].Input = 10
	if err = s.SavePrices(ctx, prices); err != nil {
		t.Fatal(err)
	}
	second := base
	second.Timestamp = "2026-09-09T02:00:00Z"
	second.Failed = true
	second.StatusCode = 429
	second.Model = "unknown"
	if err = s.Append(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	f := Filter{Start: "2026-09-09T09:00:00+08:00", End: "2026-09-09T03:00:00Z", PageSize: 1}
	if err = f.Validate(); err != nil {
		t.Fatal(err)
	}
	r, err := s.Query(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != 2 || r.Stats.Unpriced != 1 || r.Stats.Cost != 9.5 || len(r.Items) != 1 {
		t.Fatalf("unexpected aggregate: %+v", r)
	}
	snapshot := r.Snapshot
	if err = s.Append(ctx, base); err != nil {
		t.Fatal(err)
	}
	f.Snapshot = snapshot
	f.Page = 2
	r, err = s.Query(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != 2 || r.Items[0].Cost == nil || *r.Items[0].Cost != 9.5 || r.Items[0].Price.Input != 2 {
		t.Fatalf("snapshot changed: %+v", r)
	}
	f.Status = "failed"
	f.Page = 1
	f.PageSize = 25
	r, err = s.Query(ctx, f)
	if err != nil || r.Total != 1 {
		t.Fatalf("filter: %+v %v", r, err)
	}
	n, err := s.Delete(ctx, f)
	if err != nil || n != 1 {
		t.Fatalf("cleanup %d %v", n, err)
	}
	if _, err = s.Delete(ctx, Filter{}); err == nil {
		t.Fatal("unbounded cleanup accepted")
	}
	options, err := s.Options(ctx)
	if err != nil || len(options.Accounts) != 1 || options.Accounts[0].Name != "Account A" {
		t.Fatalf("options %+v %v", options, err)
	}
}

func TestPluginCanonicalTokensAndSecretExclusion(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := Plugin{Store: s}
	p.HandleUsage(context.Background(), usage.Record{RequestedAt: time.Now(), Provider: "claude", Model: "m", APIKey: "secret-api-key", AuthID: "credential.json", Fail: usage.Failure{Body: "secret-body"}, Detail: usage.Detail{InputTokens: 100, OutputTokens: 30, CacheReadTokens: 40, CacheCreationTokens: 10, ReasoningTokens: 12, TotalTokens: 192}})
	f := Filter{}
	_ = f.Validate()
	r, err := s.Query(context.Background(), f)
	if err != nil || len(r.Items) != 1 {
		t.Fatalf("query %+v %v", r, err)
	}
	record := r.Items[0]
	if record.TotalTokens != 192 || record.InputTokens != 100 || record.OutputTokens != 42 || record.CacheReadTokens != 40 || record.CacheWriteTokens != 10 {
		t.Fatalf("unexpected token breakdown %+v", record)
	}
	raw, _ := json.Marshal(record)
	if strings.Contains(string(raw), "secret-") || record.APIKeyID == "" {
		t.Fatalf("secret handling: %s", raw)
	}
	if record.Cost != nil {
		t.Fatal("unknown price presented as zero")
	}
}

func TestValidationAndNonDestructiveReads(t *testing.T) {
	for _, f := range []Filter{{Sort: "timestamp; DROP TABLE usage_records"}, {Start: "bad"}, {Status: "bad"}, {PageSize: 1001}, {Order: "DROP"}, {Start: "2026-09-10T00:00:00Z", End: "2026-09-09T00:00:00Z"}} {
		if err := f.Validate(); err == nil {
			t.Fatalf("accepted %+v", f)
		}
	}
	s, err := Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SavePrices(context.Background(), []Price{{Model: "m", Input: -1}}); err == nil {
		t.Fatal("negative price accepted")
	}
	if err = s.Append(context.Background(), Record{Timestamp: "2026-09-09T00:00:00Z", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	f := Filter{}
	_ = f.Validate()
	for i := 0; i < 2; i++ {
		r, e := s.Query(context.Background(), f)
		if e != nil || r.Total != 1 {
			t.Fatalf("destructive read %+v %v", r, e)
		}
	}
}
