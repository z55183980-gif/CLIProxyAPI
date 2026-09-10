package auth

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// sub2api429Cooldown follows ratelimit_service.go's window selection. In
// particular, Claude reset times do not receive the legacy random jitter.
func sub2api429Cooldown(provider string, headers http.Header, body string, now time.Time) (time.Duration, bool) {
	h := make(http.Header, len(headers))
	for key, values := range headers {
		h[http.CanonicalHeaderKey(key)] = values
	}
	if provider == "codex" {
		primary, secondary := "primary", "secondary"
		p, pe := strconv.Atoi(h.Get("x-codex-primary-window-minutes"))
		s, se := strconv.Atoi(h.Get("x-codex-secondary-window-minutes"))
		primaryShort := pe == nil && se == nil && p < s || pe == nil && se != nil && p <= 360 || pe != nil && se == nil && s > 360
		if primaryShort {
			primary, secondary = secondary, primary
		}

		for _, window := range []string{primary, secondary} {
			reset, err := strconv.Atoi(h.Get("x-codex-" + window + "-reset-after-seconds"))
			used, _ := strconv.ParseFloat(h.Get("x-codex-"+window+"-used-percent"), 64)
			if err == nil && used >= 100 {
				return time.Duration(reset) * time.Second, false
			}
		}
	}
	exceeded := func(window string) bool {
		prefix := "anthropic-ratelimit-unified-" + window + "-"
		used, _ := strconv.ParseFloat(h.Get(prefix+"utilization"), 64)
		return strings.EqualFold(h.Get(prefix+"surpassed-threshold"), "true") || used >= 1-1e-9
	}
	rejected := func(window string) bool {
		return strings.EqualFold(strings.TrimSpace(h.Get("anthropic-ratelimit-unified-"+window+"-status")), "rejected")
	}
	parseValid := func(raw string, limit time.Duration) (time.Time, bool) {
		stamp, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return time.Time{}, false
		}
		if stamp > 1e11 {
			stamp /= 1000
		}
		reset := time.Unix(stamp, 0)
		return reset, reset.After(now) && !reset.After(now.Add(limit))
	}
	if provider == "claude" {
		for _, window := range []string{"7d", "5h"} {
			limit := 8 * 24 * time.Hour
			if window == "5h" {
				limit = 6 * time.Hour
			}
			if reset, ok := parseValid(h.Get("anthropic-ratelimit-unified-"+window+"-reset"), limit); ok && (exceeded(window) || window == "5h" && rejected(window)) {
				return reset.Sub(now), false
			}
		}
		if exceeded("7d_oi") || rejected("7d_oi") {
			reset, ok := parseValid(h.Get("anthropic-ratelimit-unified-7d_oi-reset"), 8*24*time.Hour)
			if !ok {
				reset, ok = parseValid(h.Get("anthropic-ratelimit-unified-reset"), 8*24*time.Hour)
			}
			if ok {
				return reset.Sub(now), true
			}
		}
	}
	stamp5, err5 := strconv.ParseInt(h.Get("anthropic-ratelimit-unified-5h-reset"), 10, 64)
	stamp7, err7 := strconv.ParseInt(h.Get("anthropic-ratelimit-unified-7d-reset"), 10, 64)
	var chosen int64
	switch {
	case exceeded("7d") && err7 == nil:
		chosen = stamp7
	case exceeded("5h") && err5 == nil:
		chosen = stamp5
	case !exceeded("5h") && !exceeded("7d"):
		if err5 == nil {
			chosen = stamp5
		}
		if err7 == nil && (chosen == 0 || stamp7 < chosen) {
			chosen = stamp7
		}
	}
	if chosen != 0 {
		return time.Unix(chosen, 0).Sub(now), false
	}
	if raw := h.Get("anthropic-ratelimit-unified-reset"); raw != "" {
		if stamp, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return time.Unix(stamp, 0).Sub(now), false
		}
	}
	if provider == "codex" {
		prefix := "error."
		if !gjson.Get(body, "error").Exists() {
			prefix = "response.error."
		}
		typ := gjson.Get(body, prefix+"type").String()
		if typ == "usage_limit_reached" || typ == "rate_limit_exceeded" {
			if value := gjson.Get(body, prefix+"resets_at"); value.Exists() {
				if stamp, err := strconv.ParseInt(value.String(), 10, 64); err == nil {
					return time.Unix(stamp, 0).Sub(now), false
				}
			}
			if value := gjson.Get(body, prefix+"resets_in_seconds"); value.Exists() {
				if seconds, err := strconv.ParseInt(value.String(), 10, 64); err == nil {
					return time.Duration(seconds) * time.Second, false
				}
			}
		}
	}
	return 5 * time.Second, false
}
