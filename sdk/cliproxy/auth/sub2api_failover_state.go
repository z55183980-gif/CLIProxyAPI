package auth

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

type sub2apiTransientKey struct{ auth, model string }
type sub2apiTransientEntry struct {
	count int
	last  time.Time
}
type sub2apiTransientState struct {
	mu      sync.Mutex
	entries map[sub2apiTransientKey]sub2apiTransientEntry
}

type sub2apiForbiddenEntry struct {
	count int
	start time.Time
}
type sub2apiForbiddenState struct {
	mu      sync.Mutex
	entries map[string]sub2apiForbiddenEntry
}

func (s *sub2apiForbiddenState) record(id string, success bool, now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[string]sub2apiForbiddenEntry)
	}
	if success {
		delete(s.entries, id)
		return 0
	}
	entry := s.entries[id]
	if entry.start.IsZero() || now.Sub(entry.start) >= 180*time.Minute {
		entry = sub2apiForbiddenEntry{start: now}
	}
	entry.count++
	s.entries[id] = entry
	return entry.count
}

// sub2api's API-key transient state uses a 30-minute failure-streak lifetime and
// 0/10/45-second cooldowns. Successful requests clear the account/model streak.
func (s *sub2apiTransientState) record(id, model string, success bool, now time.Time) time.Duration {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || len(model) > 512 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[sub2apiTransientKey]sub2apiTransientEntry)
	}
	key := sub2apiTransientKey{id, model}
	if success {
		delete(s.entries, key)
		return 0
	}
	entry, exists := s.entries[key]
	if !exists && len(s.entries) >= 4096 {
		var oldest sub2apiTransientKey
		var stamp time.Time
		for k, v := range s.entries {
			if stamp.IsZero() || v.last.Before(stamp) {
				oldest, stamp = k, v.last
			}
		}
		delete(s.entries, oldest)
	}
	if now.Sub(entry.last) > 30*time.Minute || now.Before(entry.last) {
		entry.count = 0
	}
	entry.count++
	entry.last = now
	s.entries[key] = entry
	if entry.count >= 3 {
		return 45 * time.Second
	}
	if entry.count == 2 {
		return 10 * time.Second
	}
	return 0
}

func (m *Manager) classifySub2APIFailure(a *Auth, err error, bootstrap bool) *sub2apiFailure {
	status := sub2apiStatus(err)
	body := sub2apiErrorBody(err)
	lower := strings.ToLower(body)
	f := &sub2apiFailure{cause: err, status: status, neutral: true}
	streamFailure := sub2apiIsStreamFailure(err)
	if a.Provider == "claude" && streamFailure {
		f.status, f.retry = 403, true
		if bootstrap && sub2apiErrorField(body, "type") == "overloaded_error" {
			f.status, status = 529, 529
		} else {
			return f
		}
	}
	if a.Provider == "codex" && streamFailure {
		status = sub2apiFailedEventStatus(body)
		if status != 401 && status != 403 && status != 429 && status != 529 && !(status == 503 && sub2apiCapacityShed(body)) {
			status = 502
		}
		f.status = status
		if !sub2apiStreamFailureRetryable(body) {
			return f
		}
	}
	if status == 429 && a.Provider == "codex" && a.AuthKind() == AuthKindOAuth {
		m.sub2apiStorm.observe(time.Now())
	}
	var streamErr *sub2apiStreamReadError
	if errors.As(err, &streamErr) {
		f.status = 502
		f.retry = a.Provider == "codex" || !streamErr.cleanEOF
		f.same = a.Provider == "claude" && f.retry
		return f
	}
	if status == 0 {
		f.retry = true
		f.status = 502
		if f.retry && sub2apiPersistentTransport(err) {
			f.neutral = false
			f.cooldown = 10 * time.Minute
		}
		return f
	}
	if a.Provider == "codex" && sub2apiContextError(body) {
		return f
	}
	code := gjson.Get(body, "error.code").String()
	if code == "" {
		code = gjson.Get(body, "response.error.code").String()
	}
	if a.Provider == "codex" && strings.EqualFold(strings.TrimSpace(code), "cyber_policy") {
		return f
	}
	if a.Provider == "codex" && sub2apiAccessState(body) {
		f.retry, f.permanent, f.neutral = true, true, false
		return f
	}
	if a.Provider == "codex" && sub2apiCapacityShed(body) && status != 413 {
		f.retry, f.same, f.capacity, f.status = true, true, true, 503
		return f
	}
	f.retry = status == 401 || status == 403 || status == 429 || status >= 500
	if a.Provider == "codex" {
		f.retry = f.retry || status == 402 || status == 405 || status == 413 || sub2apiTransient(status, body) || status == 400 && sub2apiMissingModel400(body)
	}
	if status == 400 && a.Provider == "claude" {
		if cfg := m.runtimeConfigSnapshot(); cfg != nil && cfg.FailoverOn400 {
			for _, marker := range []string{"anthropic-beta", "beta feature", "requires beta", "thinking", "thought_signature", "signature", "tool_use", "tool_result", "tools"} {
				if strings.Contains(lower, marker) {
					f.retry = true
					break
				}
			}
		}
	}
	pool := sub2apiPool(a)
	handles := sub2apiHandlesStatus(a, status)
	mark := handles && (!pool || sub2apiCustomCodes(a))
	// Claude's custom-code HTTP retry exhaustion only marks OAuth 403.
	if sub2apiClaudeRetry(a, status) && a.AuthKind() != AuthKindOAuth {
		mark = false
	}
	if mark {
		switch status {
		case 400:
			if strings.Contains(lower, "organization has been disabled") || strings.Contains(lower, "identity verification is required") || a.Provider == "claude" && strings.Contains(lower, "credit balance") {
				f.permanent = true
				f.retry = true
			}
		case 401:
			f.permanent = a.AuthKind() != AuthKindOAuth || !authHasRefreshCredential(a)
			if a.Provider == "codex" && (strings.Contains(lower, "token_invalidated") || strings.Contains(lower, "token_revoked") || gjson.Get(body, "detail").String() == "Unauthorized") {
				f.permanent = true
			}
			if !f.permanent {
				f.cooldown = 10 * time.Minute
			}
		case 402, 403:
			if status == 403 && a.Provider == "codex" && (strings.HasPrefix(strings.TrimSpace(lower), "<!doctype html") || strings.HasPrefix(strings.TrimSpace(lower), "<html")) {
				break
			}
			f.permanent = true
			f.retry = true
			if status == 403 && a.Provider == "codex" && m.sub2apiForbidden.record(a.ID, false, time.Now()) < 3 {
				f.permanent = false
				f.cooldown = 10 * time.Minute
			}
		case 429:
			f.cooldown = 5 * time.Second

		case 529:
			if sub2apiCustomCodes(a) {
				f.permanent = true
			} else {
				f.cooldown = 10 * time.Minute
			}
		default:
			if sub2apiCustomCodes(a) {
				f.permanent = true
				f.retry = true
			}
		}
		normalized := strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(lower)), " ")
		if status == 404 && strings.Contains(normalized, "model") && (strings.Contains(normalized, "not found") || strings.Contains(normalized, "unknown model")) || a.Provider == "codex" && a.AuthKind() == AuthKindOAuth && status == 400 && strings.Contains(normalized, "model is not supported when using codex") {
			f.modelOnly = true
			f.permanent = false
			f.cooldown = 30 * time.Minute
			f.retry = true
		}
	}
	f.neutral = !f.permanent && f.cooldown == 0
	f.same = pool && f.retry && !f.permanent && f.cooldown == 0 && (sub2apiPoolStatus(a, status) || a.Provider == "codex" && sub2apiTransient(status, body))
	if a.Provider == "codex" && status == 413 {
		f.same = false
	}
	return f
}

func sub2apiTransientStatus(status int, body string) bool {
	switch status {
	case 500, 502, 503, 504, 520, 521, 522, 523, 524:
		return true
	case 400:
		return sub2apiTransient(status, body)
	default:
		return false
	}
}

// applySub2APIResultLocked replaces legacy blanket cooldowns for this flow.
// The caller owns m.mu and performs persistence/scheduler publication afterward.
func (m *Manager) applySub2APIResultLocked(a *Auth, result Result, f *sub2apiFailure, now time.Time) {
	cooldown := f.cooldown
	if a.Provider == "codex" && a.AuthKind() == AuthKindAPIKey && f.neutral && !f.capacity && !sub2apiIsStreamFailure(f.cause) && statusCodeFromError(f.cause) != 0 && sub2apiTransientStatus(f.status, result.Error.Message) && !(sub2apiPool(a) && sub2apiPoolStatus(a, f.status)) {
		cooldown = m.sub2apiTransient.record(a.ID, result.Model, false, now)
		f.modelOnly = true
	}
	if f.neutral && cooldown == 0 {
		return
	}
	next := now
	if f.permanent {
		next = time.Time{}
	}
	if cooldown > 0 {
		next = now.Add(cooldown).Round(0)
	}
	a.Status = StatusError
	a.StatusMessage = result.Error.Message
	a.LastError = cloneError(result.Error)
	if f.modelOnly {
		model := result.Model
		if a.Provider == "claude" && f.status == 429 {
			model = "claude-fable-5"
		}
		state := ensureModelState(a, model)
		state.Unavailable = true
		state.Status = StatusError
		state.StatusMessage = result.Error.Message
		state.LastError = cloneError(result.Error)
		state.UpdatedAt = now
		if state.NextRetryAfter.Before(next) || f.permanent {
			state.NextRetryAfter = next
		}
		updateAggregatedAvailability(a, now)
		return
	}
	a.Unavailable = true
	a.LastError.Code = "sub2api_account_block"
	if f.permanent {
		a.Disabled = true
	}
	if a.NextRetryAfter.Before(next) || f.permanent {
		a.NextRetryAfter = next
	}
	// Account-wide blocking must also cover models which already have state.
	for _, state := range a.ModelStates {
		if state == nil {
			continue
		}
		state.Unavailable = true
		state.Status = StatusError
		if state.NextRetryAfter.Before(next) || f.permanent {
			state.NextRetryAfter = next
		}
	}
	if f.status == 429 {
		applyCooldownFields(&a.Quota, QuotaState{Exceeded: true, Reason: "credential_quota", NextRecoverAt: next})
	}
}
