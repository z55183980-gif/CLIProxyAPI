package auth

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func (m *Manager) sub2apiTeamFailure(ctx context.Context, result Result, failure *sub2apiFailure) {
	if result.Success || failure.status != 402 || gjson.Get(sub2apiErrorBody(failure.cause), "detail.code").String() != "deactivated_workspace" {
		return
	}
	a, ok := m.GetByID(result.AuthID)
	if !ok || a.Provider != "codex" || a.AuthKind() != AuthKindOAuth {
		return
	}
	team := authMetadataString(a, "chatgpt_account_id")
	if team == "" {
		team = authMetadataString(a, "account_id")
	}
	team = strings.TrimSpace(team)
	if team == "" {
		return
	}
	now := time.Now()
	for {
		value, loaded := m.sub2apiTeamFailures.LoadOrStore(team, now.Add(time.Minute))
		if !loaded {
			break
		}
		if value.(time.Time).After(now) {
			return
		}
		if m.sub2apiTeamFailures.CompareAndSwap(team, value, now.Add(time.Minute)) {
			break
		}
	}
	for _, other := range m.List() {
		otherTeam := authMetadataString(other, "chatgpt_account_id")
		if otherTeam == "" {
			otherTeam = authMetadataString(other, "account_id")
		}
		if other.ID == a.ID || other.Provider != "codex" || strings.TrimSpace(otherTeam) != team {
			continue
		}
		cause := &Error{HTTPStatus: 402, Message: "Workspace deactivated (402): team-linked error"}
		flow := &sub2apiRequestState{lastFailure: &sub2apiFailure{cause: cause, status: 402, permanent: true}}
		m.MarkResult(context.WithValue(ctx, sub2apiRequestKey{}, flow), Result{AuthID: other.ID, Provider: "codex", Error: cause})
	}
}

// refineSub2APIFailure applies response-header and model-dependent policies from
// sub2api 98d8691 after the provider's semantic error has been classified.
func (m *Manager) refineSub2APIFailure(a *Auth, f *sub2apiFailure, headers http.Header, model string, now time.Time, allowRetry bool) {
	if f.status != 429 || f.neutral && !f.same {
		return
	}
	body := sub2apiErrorBody(f.cause)
	if a.Provider == "claude" && !f.neutral {
		f.cooldown, f.modelOnly = sub2api429Cooldown(a.Provider, headers, body, now)
		if strings.EqualFold(strings.TrimSpace(gjson.Get(body, "error.details.error_code").String()), "credits_required") {
			requested := strings.TrimSpace(gjson.Get(body, "error.details.model").String())
			if requested == "" {
				requested = model
			}
			if strings.Contains(strings.ToLower(requested), "fable") {
				// A shared exhausted window still blocks the whole account.
				hard := headers.Clone()
				hard.Del("anthropic-ratelimit-unified-7d_oi-status")
				hard.Del("anthropic-ratelimit-unified-7d_oi-utilization")
				hard.Del("anthropic-ratelimit-unified-reset")
				shared := false
				for _, window := range []string{"5h", "7d"} {
					prefix := "anthropic-ratelimit-unified-" + window + "-"
					used, _ := strconv.ParseFloat(hard.Get(prefix+"utilization"), 64)
					limit := 8 * 24 * time.Hour
					if window == "5h" {
						limit = 6 * time.Hour
					}
					_, valid := sub2apiResetTime(hard.Get(prefix+"reset"), now, limit)
					shared = shared || valid && (used >= 1-1e-9 || strings.EqualFold(hard.Get(prefix+"surpassed-threshold"), "true") || window == "5h" && strings.EqualFold(hard.Get(prefix+"status"), "rejected"))
				}
				if !shared {
					f.modelOnly = true
					f.cooldown = 5 * time.Second
					if reset, ok := sub2apiResetTime(headers.Get("anthropic-ratelimit-unified-reset"), now, 366*24*time.Hour); ok {
						f.cooldown = reset.Sub(now)
					}
				}
			}
		}
		return
	}
	if a.Provider != "codex" || f.neutral {
		return
	}
	spark := a.AuthKind() == AuthKindOAuth && canonicalModelKey(model) == "gpt-5.3-codex-spark"
	classificationHeaders := headers
	if sub2apiIsStreamFailure(f.cause) && !spark {
		classificationHeaders = nil
	}
	quota, windowQuota := sub2apiCodexQuota(classificationHeaders, body)
	f.cooldown, f.modelOnly = sub2api429Cooldown("codex", classificationHeaders, body, now)
	if spark {
		f.modelOnly = true
		if !windowQuota || f.cooldown <= 0 {
			f.cooldown = 5 * time.Second
		}
		return
	}
	if a.AuthKind() != AuthKindOAuth {
		return
	}
	if !quota && allowRetry && !a.Disabled && !a.NextRetryAfter.After(now) {
		value, _ := m.sub2apiOAuth429.LoadOrStore(a.ID, now)
		start, _ := value.(time.Time)
		deadline := start.Add(2 * time.Minute)
		if now.Before(deadline) {
			f.neutral, f.same, f.cooldown = true, true, 0
			f.retryDeadline = deadline
			f.retryDelay = 500 * time.Millisecond
			if raw := headers.Get("Retry-After"); raw != "" {
				if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds > 0 {
					f.retryDelay = time.Duration(seconds * float64(time.Second))
				} else if reset, err := http.ParseTime(raw); err == nil && reset.After(now) {
					f.retryDelay = reset.Sub(now)
				}
			}
			f.retryDelay = min(f.retryDelay, 8*time.Second, deadline.Sub(now))
			return
		}
	}
	if f.cooldown <= 0 {
		f.cooldown = 5 * time.Second
	}
	m.sub2apiOAuth429.Delete(a.ID)
}

func sub2apiResetTime(raw string, now time.Time, maximum time.Duration) (time.Time, bool) {
	stamp, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	if stamp > 1e11 {
		stamp /= 1000
	}
	reset := time.Unix(stamp, 0)
	return reset, reset.After(now) && !reset.After(now.Add(maximum))
}

func sub2apiCodexQuota(headers http.Header, body string) (bool, bool) {
	for _, window := range []string{"primary", "secondary"} {
		if used, err := strconv.ParseFloat(headers.Get("x-codex-"+window+"-used-percent"), 64); err == nil && used >= 100 {
			return true, true
		}
	}
	prefix := "error."
	if !gjson.Get(body, "error").Exists() {
		prefix = "response.error."
	}
	typ := gjson.Get(body, prefix+"type").String()
	if typ == "usage_limit_reached" || typ == "rate_limit_exceeded" {
		for _, name := range []string{"resets_at", "resets_in_seconds"} {
			if _, err := strconv.ParseInt(gjson.Get(body, prefix+name).String(), 10, 64); err == nil {
				return true, false
			}
		}
	}
	return false, false
}
