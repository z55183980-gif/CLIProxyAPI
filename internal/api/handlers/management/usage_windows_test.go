package management

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usagehistory"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type usageWindowTestClient func(*http.Request) (*http.Response, error)

func (f usageWindowTestClient) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestUsageWindowsActiveRequestsAndCache(t *testing.T) {
	for _, provider := range []string{"claude"} {
		t.Run(provider, func(t *testing.T) {
			manager := coreauth.NewManager(nil, nil, nil)
			auth, err := manager.Register(context.Background(), &coreauth.Auth{ID: provider, Provider: provider, Metadata: map[string]any{"access_token": "fixture", "account_id": "fixture-account"}})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			h := &Handler{authManager: manager, usageWindowHTTPClient: usageWindowTestClient(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Header.Get("Authorization") != "Bearer fixture" {
					t.Fatal("missing account authorization")
				}
				header := make(http.Header)
				body := `{"five_hour":{"utilization":17,"resets_at":"2099-01-01T00:00:00Z"},"seven_day_overage_included":{"utilization":34,"resets_at":"2099-01-02T00:00:00Z"}}`
				if req.Method != "GET" || req.URL.Path != "/api/oauth/usage" || req.Header.Get("Anthropic-Beta") == "" {
					t.Fatalf("wrong Claude query: %s %s", req.Method, req.URL)
				}
				return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			query := func(source string) accountUsageWindows {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodGet, "/auth-files/usage-windows?auth_index="+auth.Index+"&source="+source, nil)
				h.GetAccountUsageWindows(c)
				var result accountUsageWindows
				if w.Code != 200 {
					t.Fatalf("response=%d %s", w.Code, w.Body)
				}
				if errDecode := json.Unmarshal(w.Body.Bytes(), &result); errDecode != nil {
					t.Fatal(errDecode)
				}
				return result
			}
			if provider == "claude" {
				query("passive")
				if calls != 0 {
					t.Fatal("Claude passive called upstream")
				}
			}
			active := query("active")
			if active.Windows[0].UsedPercent != 17 {
				t.Fatalf("active=%+v", active)
			}
			passive := query("passive")
			if passive.Windows[0].UsedPercent != 17 || calls != 1 {
				t.Fatalf("cached passive=%+v calls=%d", passive, calls)
			}
			if provider == "claude" {
				query("active")
				if calls != 1 || !hasUsageWindow(passive.Windows, "seven-day-fable") {
					t.Fatalf("Claude cache failed: calls=%d", calls)
				}
			}
			live, _ := manager.GetByID(provider)
			if len(live.Quota.Signals) != 0 || live.Quota.Exceeded {
				t.Fatal("UI probe modified scheduler state")
			}
		})
	}
}

func TestCodexObservedWindowsClassifiesAndExpires(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 30, 0, 0, time.UTC)
	signals := map[string]string{
		"x-codex-primary-used-percent": "62", "x-codex-primary-window-minutes": "10080",
		"x-codex-primary-reset-after-seconds": "3600",
		"x-codex-secondary-used-percent":      "24", "x-codex-secondary-window-minutes": "300",
		"x-codex-secondary-reset-at": strconv.FormatInt(now.Add(-time.Second).Unix(), 10),
	}
	windows := codexObservedWindows(signals, now.Add(-time.Minute), now)
	if windows[0].UsedPercent != 0 || windows[1].UsedPercent != 62 || !windows[1].ResetAt.Equal(now.Add(59*time.Minute)) {
		t.Fatalf("windows=%+v", windows)
	}
	delete(signals, "x-codex-secondary-used-percent")
	if got := codexObservedWindows(signals, now, now); got[1].UsedPercent != 62 {
		t.Fatalf("missing secondary overwrote weekly: %+v", got)
	}
	signals["x-codex-primary-used-percent"] = "NaN"
	if got := codexObservedWindows(signals, now, now); got[1].UsedPercent != 0 {
		t.Fatalf("invalid percentage: %+v", got)
	}
}

func TestChannelWindowStartFallbacks(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 30, 0, 0, time.UTC)
	expired := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	for _, tc := range []struct {
		provider, id string
		reset        *time.Time
		want         time.Time
	}{
		{"codex", "five-hour", nil, now.Add(-5 * time.Hour)},
		{"codex", "weekly", &expired, now.Add(-7 * 24 * time.Hour)},
		{"codex", "weekly", &future, future.Add(-7 * 24 * time.Hour)},
		{"claude", "five-hour", nil, now.Truncate(time.Hour)},
		{"claude", "five-hour", &expired, now.Truncate(time.Hour)},
		{"claude", "five-hour", &future, future.Add(-5 * time.Hour)},
		{"claude", "seven-day", &future, future.Add(-7 * 24 * time.Hour)},
		{"claude", "seven-day", nil, now.Add(-7 * 24 * time.Hour)},
		{"claude", "seven-day", &expired, now.Add(-7 * 24 * time.Hour)},
	} {
		if got := usageWindowStart(tc.provider, accountUsageWindow{ID: tc.id, ResetAt: tc.reset}, now); !got.Equal(tc.want) {
			t.Fatalf("%s %s: %v != %v", tc.provider, tc.id, got, tc.want)
		}
	}
}

func TestClaudePassiveAndActiveWindows(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 30, 0, 0, time.UTC)
	signals := map[string]string{
		"anthropic-ratelimit-unified-5h-status":         "allowed_warning",
		"anthropic-ratelimit-unified-5h-reset":          strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
		"anthropic-ratelimit-unified-7d_oi-utilization": "0.41",
		"anthropic-ratelimit-unified-7d_oi-reset":       strconv.FormatInt(now.Add(48*time.Hour).Unix(), 10),
	}
	passive := claudeObservedWindows(signals, now, now)
	if len(passive) != 2 || passive[0].UsedPercent != 80 || passive[1].Label != "7d F" {
		t.Fatalf("passive=%+v", passive)
	}
	active, err := claudeActiveWindows([]byte(`{"five_hour":{"utilization":12,"resets_at":"2026-09-09T14:00:00Z"},"seven_day":{"utilization":20,"resets_at":"2026-09-12T14:00:00Z"},"seven_day_sonnet":{"utilization":30,"resets_at":"2026-09-12T14:00:00Z"}}`))
	if err != nil || len(active) != 3 || active[2].Label != "7d S" {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	updated := claudeObservedWindows(claudeWindowSignals(active, signals), now, now)
	if updated[0].UsedPercent != 12 || !hasUsageWindow(updated, "seven-day-fable") {
		t.Fatalf("active sync lost values: %+v", updated)
	}
	expired := claudeObservedWindows(signals, now, now.Add(6*time.Hour))
	if expired[0].UsedPercent != 0 || expired[0].ResetAt != nil || expired[1].UsedPercent != 41 {
		t.Fatalf("expiration=%+v", expired)
	}
	if _, err := claudeActiveWindows([]byte(`{"five_hour":"bad"}`)); err == nil {
		t.Fatal("malformed active response accepted")
	}
}

func TestAccountUsageWindowsSeparatesChannelStatistics(t *testing.T) {
	now := time.Now()
	store, err := usagehistory.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	usagehistory.SetActive(store)
	t.Cleanup(func() {
		if errClose := store.Close(); errClose != nil {
			t.Error(errClose)
		}
	})
	manager := coreauth.NewManager(nil, nil, nil)
	for _, provider := range []string{"codex", "claude"} {
		auth := &coreauth.Auth{ID: provider, Provider: provider, Metadata: map[string]any{"access_token": "fixture"}}
		auth.Quota = coreauth.QuotaState{ObservedAt: now, Signals: map[string]string{
			"X-Codex-Primary-Used-Percent": "20", "X-Codex-Primary-Window-Minutes": "300", "X-Codex-Primary-Reset-At": strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
			"X-Codex-Secondary-Used-Percent": "30", "X-Codex-Secondary-Window-Minutes": "10080", "X-Codex-Secondary-Reset-At": strconv.FormatInt(now.Add(24*time.Hour).Unix(), 10),
			"Anthropic-Ratelimit-Unified-5h-Utilization": "0.2", "Anthropic-Ratelimit-Unified-5h-Reset": strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
			"Anthropic-Ratelimit-Unified-7d-Utilization": "0.3", "Anthropic-Ratelimit-Unified-7d-Reset": strconv.FormatInt(now.Add(24*time.Hour).Unix(), 10),
		}}
		registered, errRegister := manager.Register(context.Background(), auth)
		if errRegister != nil {
			t.Fatal(errRegister)
		}
		for _, age := range []time.Duration{time.Hour, 24 * time.Hour} {
			cost := 1.0
			errAppend := store.Append(context.Background(), usagehistory.Record{AccountID: provider, Provider: provider, Timestamp: now.Add(-age).Format(time.RFC3339Nano), TotalTokens: 100, Cost: &cost})
			if errAppend != nil {
				t.Fatal(errAppend)
			}
		}
		h := &Handler{authManager: manager}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/auth-files/usage-windows?auth_index="+registered.Index, nil)
		h.GetAccountUsageWindows(c)
		if w.Code != 200 {
			t.Fatalf("response %d: %s", w.Code, w.Body)
		}
		var result accountUsageWindows
		if errDecode := json.Unmarshal(w.Body.Bytes(), &result); errDecode != nil {
			t.Fatal(errDecode)
		}
		if len(result.Windows) != 2 || result.Windows[0].Stats == nil || result.Windows[0].Stats.Requests != 1 {
			t.Fatalf("%s stats=%+v", provider, result)
		}
		if result.Windows[1].Stats == nil || result.Windows[1].Stats.Requests != 2 || result.Windows[1].Stats.Tokens != 200 || result.Windows[1].Stats.Cost != 2 {
			t.Fatalf("weekly stats=%+v", result)
		}
	}
}

func TestAccountUsageWindowsActiveRefreshReadsNewStatistics(t *testing.T) {
	store, err := usagehistory.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	previous := usagehistory.Active()
	usagehistory.SetActive(store)
	t.Cleanup(func() {
		usagehistory.SetActive(previous)
		if errClose := store.Close(); errClose != nil {
			t.Error(errClose)
		}
	})
	manager := coreauth.NewManager(nil, nil, nil)
	now := time.Now()
	auth, err := manager.Register(context.Background(), &coreauth.Auth{
		ID: "claude-refresh", Provider: "claude",
		Metadata: map[string]any{"type": "setup-token", "access_token": "fixture"},
		Quota: coreauth.QuotaState{ObservedAt: now, Signals: map[string]string{
			"Anthropic-Ratelimit-Unified-5h-Reset": strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{authManager: manager}
	query := func(source string) usagehistory.AccountStats {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/auth-files/usage-windows?auth_index="+auth.Index+"&source="+source, nil)
		h.GetAccountUsageWindows(c)
		var result accountUsageWindows
		if errDecode := json.Unmarshal(w.Body.Bytes(), &result); errDecode != nil {
			t.Fatal(errDecode)
		}
		if w.Code != http.StatusOK || result.StatsError != "" || len(result.Windows) == 0 || result.Windows[0].Stats == nil {
			t.Fatalf("unexpected response: %s", w.Body)
		}
		return *result.Windows[0].Stats
	}
	if stats := query("passive"); stats.Requests != 0 {
		t.Fatalf("initial statistics: %+v", stats)
	}
	cost := 0.001
	if errAppend := store.Append(context.Background(), usagehistory.Record{
		AccountID: auth.ID, Provider: "claude", Timestamp: now.Format(time.RFC3339Nano), TotalTokens: 482, Cost: &cost,
	}); errAppend != nil {
		t.Fatal(errAppend)
	}
	if stats := query("passive"); stats.Requests != 0 {
		t.Fatalf("passive statistics should use the minute cache: %+v", stats)
	}
	if stats := query("active"); stats.Requests != 1 || stats.Tokens != 482 {
		t.Fatalf("active query did not read the new request: %+v", stats)
	}
}

// Codex window refreshes read observed quota data without generating upstream traffic.
func TestCodexUsageWindowsNeverProbesUpstream(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	auth, errRegister := manager.Register(context.Background(), &coreauth.Auth{
		ID: "passive-codex", Provider: "codex", Metadata: map[string]any{"access_token": "fixture"},
	})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	h := &Handler{authManager: manager, usageWindowHTTPClient: usageWindowTestClient(func(*http.Request) (*http.Response, error) {
		t.Fatal("Codex usage window refresh must not call upstream")
		return nil, nil
	})}
	for _, observed := range []bool{false, true} {
		if observed {
			auth.Quota = coreauth.QuotaState{ObservedAt: time.Now(), Signals: map[string]string{
				"X-Codex-Primary-Used-Percent": "17", "X-Codex-Primary-Window-Minutes": "300",
				"X-Codex-Primary-Reset-After-Seconds": "3600",
			}}
			if _, errUpdate := manager.Update(context.Background(), auth); errUpdate != nil {
				t.Fatal(errUpdate)
			}
		}
		for _, source := range []string{"passive", "active", "active"} {
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/auth-files/usage-windows?auth_index="+auth.Index+"&source="+source, nil)
			h.GetAccountUsageWindows(ctx)
			var result accountUsageWindows
			if errDecode := json.Unmarshal(rec.Body.Bytes(), &result); errDecode != nil {
				t.Fatal(errDecode)
			}
			want := float64(0)
			if observed {
				want = 17
			}
			if rec.Code != 200 || result.Source != "passive" || len(result.Windows) != 2 || result.Windows[0].UsedPercent != want {
				t.Fatalf("observed=%v source=%s result=%s", observed, source, rec.Body.String())
			}
		}
	}
}
