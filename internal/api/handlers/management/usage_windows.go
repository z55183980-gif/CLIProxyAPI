package management

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usagehistory"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

type accountUsageWindow struct {
	ID          string                     `json:"id"`
	Label       string                     `json:"label"`
	UsedPercent float64                    `json:"usedPercent"`
	ResetAt     *time.Time                 `json:"resetAt,omitempty"`
	Stats       *usagehistory.AccountStats `json:"stats,omitempty"`
}

type accountUsageWindows struct {
	Windows    []accountUsageWindow `json:"windows"`
	Source     string               `json:"source"`
	Error      string               `json:"error,omitempty"`
	StatsError string               `json:"statsError,omitempty"`
}

// Each credential serializes probes. Cached active samples never alter scheduler state.
type usageWindowCache struct {
	sync.Mutex
	identity   [32]byte
	observedAt time.Time
	signals    map[string]string
	active     []accountUsageWindow
	activeAt   time.Time
	activeErr  error
	probeAt    time.Time
	stats      map[string]usageWindowStatsCache
}

type usageWindowStatsCache struct {
	stats *usagehistory.AccountStats
	at    time.Time
	start time.Time
}

func (h *Handler) GetAccountUsageWindows(c *gin.Context) {
	auth := h.authByIndex(c.Query("auth_index"))
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
		return
	}
	if auth.Provider != "codex" && auth.Provider != "claude" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "usage windows require Codex or Claude"})
		return
	}
	if auth.Disabled {
		c.JSON(http.StatusConflict, gin.H{"error": "account is disabled"})
		return
	}
	entry, _ := h.usageWindows.LoadOrStore(auth.ID, &usageWindowCache{})
	cache := entry.(*usageWindowCache)
	cache.Lock()
	defer cache.Unlock()
	now := time.Now()
	identity := sha256.Sum256([]byte(auth.Provider + "\x00" + auth.ProxyURL + "\x00" + tokenValueForAuth(auth)))
	if cache.identity != identity {
		cache.identity, cache.signals, cache.active = identity, nil, nil
		cache.observedAt, cache.activeAt, cache.probeAt = time.Time{}, time.Time{}, time.Time{}
		cache.activeErr = nil
		cache.stats = nil
	}
	if auth.Quota.ObservedAt.After(cache.observedAt) {
		cache.signals = auth.Quota.Clone().Signals
		cache.observedAt = auth.Quota.ObservedAt
	}
	force := c.Query("source") == "active"
	result := accountUsageWindows{Source: "passive"}
	if auth.Provider == "codex" {
		windows := codexObservedWindows(cache.signals, cache.observedAt, now)
		missing := windows[0].ResetAt == nil || windows[1].ResetAt == nil
		websockets, _ := authWebsocketsValue(auth)
		stale := websockets && now.Sub(cache.observedAt) >= 10*time.Minute
		if force || ((missing || stale || auth.Quota.Exceeded) && now.Sub(cache.probeAt) >= 10*time.Minute) {
			cache.probeAt = now
			headers, _, errProbe := h.requestUsageWindow(c.Request.Context(), auth, true)
			if errProbe == nil {
				var observed coreauth.QuotaState
				if observed.ObserveResponseHeadersForProvider("codex", headers, now) {
					cache.signals, cache.observedAt = observed.Signals, now
					windows = codexObservedWindows(cache.signals, now, now)
				} else {
					result.Error = "upstream returned no quota snapshot"
				}
			} else {
				result.Error = errProbe.Error()
			}
		}
		result.Windows = windows
	} else {
		result.Windows = claudeObservedWindows(cache.signals, cache.observedAt, now)
		if force && !isClaudeSetupToken(auth) {
			result.Source = "active"
			ttl := 3 * time.Minute
			if cache.activeErr != nil {
				ttl = time.Minute
			}
			if now.Sub(cache.activeAt) >= ttl {
				_, body, errFetch := h.requestUsageWindow(c.Request.Context(), auth, false)
				cache.activeAt, cache.activeErr = now, errFetch
				if errFetch == nil {
					cache.active, cache.activeErr = claudeActiveWindows(body)
					if cache.activeErr == nil {
						cache.signals = claudeWindowSignals(cache.active, cache.signals)
						cache.observedAt = now
					}
				}
			}
			if cache.activeErr != nil {
				result.Error = cache.activeErr.Error()
			} else {
				result.Windows = append([]accountUsageWindow(nil), cache.active...)
				if !hasUsageWindow(result.Windows, "seven-day-fable") {
					for _, w := range claudeObservedWindows(cache.signals, cache.observedAt, now) {
						if w.ID == "seven-day-fable" {
							result.Windows = append(result.Windows, w)
						}
					}
				}
			}
		}
	}
	if store := usagehistory.Active(); store != nil {
		for i := range result.Windows {
			w := &result.Windows[i]
			if w.ID != "five-hour" && w.ID != "weekly" && w.ID != "seven-day" {
				continue
			}
			start := usageWindowStart(auth.Provider, *w, now)
			cached := cache.stats[w.ID]
			if !force && auth.Provider == "claude" && cached.stats != nil && now.Sub(cached.at) < time.Minute && start.Equal(cached.start) {
				w.Stats = cached.stats
				continue
			}
			stats, errStats := store.AccountWindowStats(c.Request.Context(), []string{auth.ID}, start.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
			if errStats != nil {
				result.StatsError = "could not query window statistics"
				continue
			}
			s := stats[auth.ID]
			w.Stats = &s
			if auth.Provider == "claude" {
				if cache.stats == nil {
					cache.stats = make(map[string]usageWindowStatsCache)
				}
				cache.stats[w.ID] = usageWindowStatsCache{stats: &s, at: now, start: start}
			}
		}
	} else {
		result.StatsError = "usage history is unavailable"
	}
	c.JSON(http.StatusOK, result)
}

func hasUsageWindow(windows []accountUsageWindow, id string) bool {
	for _, w := range windows {
		if w.ID == id {
			return true
		}
	}
	return false
}

func isClaudeSetupToken(auth *coreauth.Auth) bool {
	for _, key := range []string{"type", "account_type"} {
		if auth.Metadata[key] == "setup-token" {
			return true
		}
	}
	return false
}

func signalNumber(signals map[string]string, key string) (float64, bool) {
	for k, raw := range signals {
		if !strings.EqualFold(k, key) {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

func signalTime(signals map[string]string, key string) *time.Time {
	v, ok := signalNumber(signals, key)
	if !ok || v <= 0 {
		return nil
	}
	if v > 1e11 {
		v /= 1000
	}
	t := time.Unix(int64(v), 0).UTC()
	return &t
}

func codexObservedWindows(signals map[string]string, observed, now time.Time) []accountUsageWindow {
	windows := []accountUsageWindow{{ID: "five-hour", Label: "5h"}, {ID: "weekly", Label: "7d"}}
	for i, slot := range []string{"primary", "secondary"} {
		prefix := "x-codex-" + slot + "-"
		used, hasUsed := signalNumber(signals, prefix+"used-percent")
		if !hasUsed {
			continue
		}
		index := i
		if minutes, ok := signalNumber(signals, prefix+"window-minutes"); ok {
			if minutes == 300 {
				index = 0
			} else if minutes == 10080 {
				index = 1
			} else {
				continue
			}
		}
		w := &windows[index]
		w.UsedPercent = used
		w.ResetAt = signalTime(signals, prefix+"reset-at")
		if w.ResetAt == nil && !observed.IsZero() {
			if seconds, ok := signalNumber(signals, prefix+"reset-after-seconds"); ok && seconds > 0 {
				r := observed.Add(time.Duration(seconds) * time.Second)
				w.ResetAt = &r
			}
		}
		if w.ResetAt != nil && !now.Before(*w.ResetAt) {
			w.UsedPercent = 0
		}
	}
	return windows
}

func claudeObservedWindows(signals map[string]string, observed, now time.Time) []accountUsageWindow {
	windows := []accountUsageWindow{}
	for i, spec := range []struct{ key, id, label string }{{"5h", "five-hour", "5h"}, {"7d", "seven-day", "7d"}, {"7d_oi", "seven-day-fable", "7d F"}} {
		prefix := "anthropic-ratelimit-unified-" + spec.key + "-"
		util, found := signalNumber(signals, prefix+"utilization")
		reset := signalTime(signals, prefix+"reset")
		if i > 0 && util <= 0 && reset == nil {
			continue
		}
		w := accountUsageWindow{ID: spec.id, Label: spec.label, UsedPercent: util * 100, ResetAt: reset}
		if i == 0 {
			status := ""
			for k, v := range signals {
				if strings.EqualFold(k, prefix+"status") {
					status = v
				}
			}
			if reset == nil && !observed.IsZero() && (status == "allowed" || status == "allowed_warning") {
				r := observed.Truncate(time.Hour).Add(5 * time.Hour)
				w.ResetAt = &r
			}
			if !found {
				if status == "rejected" {
					w.UsedPercent = 100
				} else if status == "allowed_warning" {
					w.UsedPercent = 80
				}
			}
			if w.ResetAt == nil || !now.Before(*w.ResetAt) {
				w.UsedPercent, w.ResetAt = 0, nil
			}
		}
		windows = append(windows, w)
	}
	return windows
}

func usageWindowStart(provider string, w accountUsageWindow, now time.Time) time.Time {
	period := 5 * time.Hour
	if w.ID == "weekly" || w.ID == "seven-day" {
		period = 7 * 24 * time.Hour
	}
	if w.ResetAt != nil && now.Before(*w.ResetAt) {
		return w.ResetAt.Add(-period)
	}
	if provider == "claude" && w.ID == "five-hour" {
		return now.Truncate(time.Hour)
	}
	return now.Add(-period)
}

func claudeActiveWindows(body []byte) ([]accountUsageWindow, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return nil, fmt.Errorf("invalid Claude usage response")
	}
	windows := []accountUsageWindow{}
	for i, spec := range []struct{ key, id, label string }{{"five_hour", "five-hour", "5h"}, {"seven_day", "seven-day", "7d"}, {"seven_day_sonnet", "seven-day-sonnet", "7d S"}, {"seven_day_overage_included", "seven-day-fable", "7d F"}} {
		var raw struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		}
		if data := payload[spec.key]; len(data) > 0 {
			if err := json.Unmarshal(data, &raw); err != nil {
				return nil, fmt.Errorf("invalid Claude usage window")
			}
		}
		if i > 0 && raw.ResetsAt == "" {
			continue
		}
		w := accountUsageWindow{ID: spec.id, Label: spec.label, UsedPercent: raw.Utilization}
		if reset, err := time.Parse(time.RFC3339Nano, raw.ResetsAt); err == nil {
			w.ResetAt = &reset
		}
		windows = append(windows, w)
	}
	return windows, nil
}

func claudeWindowSignals(windows []accountUsageWindow, previous map[string]string) map[string]string {
	signals := make(map[string]string)
	for k, v := range previous {
		signals[http.CanonicalHeaderKey(k)] = v
	}
	for _, w := range windows {
		key := map[string]string{"five-hour": "5h", "seven-day": "7d", "seven-day-fable": "7d_oi"}[w.ID]
		if key == "" {
			continue
		}
		prefix := "Anthropic-Ratelimit-Unified-" + key + "-"
		signals[http.CanonicalHeaderKey(prefix+"Utilization")] = strconv.FormatFloat(w.UsedPercent/100, 'f', -1, 64)
		if w.ResetAt != nil {
			signals[http.CanonicalHeaderKey(prefix+"Reset")] = strconv.FormatInt(w.ResetAt.Unix(), 10)
		}
	}
	return signals
}

func (h *Handler) requestUsageWindow(ctx context.Context, auth *coreauth.Auth, codex bool) (http.Header, []byte, error) {
	token, errToken := h.resolveTokenForAuth(ctx, auth, "")
	if errToken != nil || token == "" {
		return nil, nil, fmt.Errorf("could not acquire account credentials")
	}
	method, url, body := http.MethodGet, "https://api.anthropic.com/api/oauth/usage", ""
	if codex {
		method, url, body = http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", `{"model":"gpt-5.4","instructions":"You are a helpful assistant.","input":[{"role":"user","content":[{"type":"input_text","text":"Hi"}]}],"stream":true,"store":false}`
	}
	req, errRequest := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if errRequest != nil {
		return nil, nil, errRequest
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if codex {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		userAgent := "codex-tui/0.153.3 (Mac OS 26.5.1; arm64) iTerm.app/3.6.11 (codex-tui; 0.153.3)"
		if h.cfg != nil && strings.TrimSpace(h.cfg.CodexHeaderDefaults.UserAgent) != "" {
			userAgent = strings.TrimSpace(h.cfg.CodexHeaderDefaults.UserAgent)
		}
		req.Header.Set("User-Agent", userAgent)
		originator := strings.SplitN(strings.Fields(userAgent)[0], "/", 2)[0]
		req.Header.Set("Originator", originator)
		if id, ok := auth.Metadata["account_id"].(string); ok && id != "" {
			req.Header.Set("Chatgpt-Account-Id", id)
		}
	} else {
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	client := h.usageWindowHTTPClient
	if client == nil {
		client = &http.Client{Transport: h.apiCallTransport(auth, "")}
	}
	resp, errDo := client.Do(req)
	if errDo != nil {
		return nil, nil, fmt.Errorf("upstream usage request failed")
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.WithError(errClose).Debug("close usage response")
		}
	}()
	if codex {
		var q coreauth.QuotaState
		if q.ObserveResponseHeadersForProvider("codex", resp.Header, time.Now()) {
			return resp.Header, nil, nil
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("upstream usage returned HTTP %d", resp.StatusCode)
	}
	if codex {
		return resp.Header, nil, nil
	}
	data, errRead := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if errRead != nil {
		return nil, nil, fmt.Errorf("could not read upstream usage")
	}
	return resp.Header, data, nil
}
