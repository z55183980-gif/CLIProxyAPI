package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// These policies follow sub2api's commit 98d86915becae9fe9491a91ffc6defd5235c8d2b:
// gateway_forward.go, failover_loop.go,
// openai_gateway_handler.go and openai_account_runtime_block_fastpath.go.
const sub2apiMaxSwitches = 10

func sub2apiRefreshPending(a *Auth) bool {
	return a != nil && !a.Disabled && a.LastError != nil && a.LastError.Code == "sub2api_account_block" && a.LastError.HTTPStatus == 401 && authHasRefreshCredential(a)
}

type sub2apiRequestKey struct{}
type sub2apiRequestState struct {
	lastFailure *sub2apiFailure
	model       string
	headers     http.Header
	now         func() time.Time
	wait        func(context.Context, time.Duration) error
	switches    int
	maxSwitches int
	sameRetries map[string]int
}

// UsesSub2APIFailover identifies routes whose retries are owned by the provider flow.
func UsesSub2APIFailover(providers []string) bool {
	if len(providers) == 0 {
		return false
	}
	for _, p := range providers {
		if p != "claude" && p != "codex" {
			return false
		}
	}
	return true
}

func (m *Manager) withSub2APIFailover(ctx context.Context, providers []string) context.Context {
	if m.HomeEnabled() || !UsesSub2APIFailover(providers) {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	limit := sub2apiMaxSwitches
	if cfg := m.runtimeConfigSnapshot(); cfg != nil && cfg.MaxAccountSwitches > 0 {
		limit = cfg.MaxAccountSwitches
	}
	return context.WithValue(ctx, sub2apiRequestKey{}, &sub2apiRequestState{maxSwitches: limit, sameRetries: make(map[string]int)})
}

func sub2apiState(ctx context.Context) *sub2apiRequestState {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(sub2apiRequestKey{}).(*sub2apiRequestState)
	return s
}

type sub2apiFailure struct {
	cause         error
	retry         bool
	status        int
	same          bool
	neutral       bool
	cooldown      time.Duration
	permanent     bool
	modelOnly     bool
	capacity      bool
	retryDeadline time.Time
	retryDelay    time.Duration
}

func (e *sub2apiFailure) Error() string {
	if e.capacity {
		return string(e.ResponseBody())
	}
	return e.cause.Error()
}
func (e *sub2apiFailure) Unwrap() error   { return e.cause }
func (e *sub2apiFailure) StatusCode() int { return e.status }
func (e *sub2apiFailure) Headers() http.Header {
	var source interface{ Headers() http.Header }
	if errors.As(e.cause, &source) {
		return source.Headers()
	}
	return nil
}
func (e *sub2apiFailure) RetryAfter() *time.Duration { return retryAfterFromError(e.cause) }
func (e *sub2apiFailure) ResponseBody() []byte {
	body := []byte(sub2apiErrorBody(e.cause))
	if e.capacity {
		for _, parent := range []string{"error", "response.error"} {
			if !gjson.GetBytes(body, parent).Exists() {
				continue
			}
			code := gjson.GetBytes(body, parent+".code").String()
			if code == "" || code == "server_is_overloaded" || code == "slow_down" {
				body, _ = sjson.SetBytes(body, parent+".code", "server_error")
			}
		}
	}
	return body
}
func (e *sub2apiFailure) IsRequestScoped() bool    { return !e.retry && e.neutral }
func (e *sub2apiFailure) IsCredentialScoped() bool { return !e.neutral && !e.modelOnly }
func sub2apiFailureFrom(err error) *sub2apiFailure {
	var e *sub2apiFailure
	errors.As(err, &e)
	return e
}

func sub2apiPool(a *Auth) bool {
	enabled, _ := a.Metadata["pool_mode"].(bool)
	return a.AuthKind() == AuthKindAPIKey && enabled
}
func sub2apiInt(v any, fallback int) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i
		}
	}
	return fallback
}
func sub2apiSameLimit(a *Auth) int {
	if !sub2apiPool(a) {
		return 3
	}
	return min(10, max(0, sub2apiInt(a.Metadata["pool_mode_retry_count"], 3)))
}
func sub2apiPoolStatus(a *Auth, status int) bool {
	values, ok := a.Metadata["pool_mode_retry_status_codes"].([]any)
	if !ok {
		return status == 401 || status == 403 || status == 429
	}
	for _, v := range values {
		n := sub2apiInt(v, 0)
		if n >= 100 && n <= 599 && n == status {
			return true
		}
	}
	return false
}
func sub2apiCustomCodes(a *Auth) bool {
	v, _ := a.Metadata["custom_error_codes_enabled"].(bool)
	return a.AuthKind() == AuthKindAPIKey && v
}
func sub2apiHandlesStatus(a *Auth, status int) bool {
	if !sub2apiCustomCodes(a) {
		return true
	}
	values, _ := a.Metadata["custom_error_codes"].([]any)
	count := 0
	for _, v := range values {
		if n, ok := v.(float64); ok {
			count++
			if int(n) == status {
				return true
			}
		}
	}
	return count == 0
}
func sub2apiClaudeRetry(a *Auth, status int) bool {
	if a.Provider != "claude" || status < 400 || status == 400 {
		return false
	}
	if a.AuthKind() == AuthKindOAuth {
		return status == 403
	}
	return !sub2apiHandlesStatus(a, status)
}
func sub2apiErrorBody(err error) string {
	var raw interface{ UpstreamResponseBody() []byte }
	if errors.As(err, &raw) {
		return string(raw.UpstreamResponseBody())
	}
	return extractErrorBody(err)
}
func sub2apiStatus(err error) int {
	var raw interface{ UpstreamStatusCode() int }
	if errors.As(err, &raw) {
		return raw.UpstreamStatusCode()
	}
	return statusCodeFromError(err)
}
func sub2apiErrorField(body, name string) string {
	for _, path := range []string{"error." + name, "response.error." + name, name} {
		if value := strings.TrimSpace(gjson.Get(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}

func sub2apiContextError(body string) bool {
	match := func(text string) bool {
		s := strings.ToLower(text)
		if strings.Contains(s, "context_too_large") || strings.Contains(s, "context_length_exceeded") || strings.Contains(s, "maximum context length") || strings.Contains(s, "max context length") {
			return true
		}
		exceeded := strings.Contains(s, "exceed") || strings.Contains(s, "too large") || strings.Contains(s, "too long")
		return exceeded && (strings.Contains(s, "context window") || strings.Contains(s, "context length") || strings.Contains(s, "token limit") && strings.Contains(s, "context"))
	}
	for _, path := range []string{"error.message", "response.error.message", "message", "error.code", "response.error.code", "code"} {
		if match(gjson.Get(body, path).String()) {
			return true
		}
	}
	return !gjson.Valid(body) && match(body)
}
func sub2apiCapacityShed(body string) bool {
	code := strings.ToLower(sub2apiErrorField(body, "code"))
	if code == "server_is_overloaded" || code == "slow_down" {
		return true
	}
	match := func(s string) bool {
		s = strings.ToLower(s)
		return strings.Contains(s, "server is overloaded") || strings.Contains(s, "servers are overloaded") || strings.Contains(s, "servers are currently overloaded")
	}
	for _, path := range []string{"error.message", "response.error.message", "message"} {
		if match(gjson.Get(body, path).String()) {
			return true
		}
	}
	return !gjson.Valid(body) && match(body)
}
func sub2apiTransient(status int, body string) bool {
	if status < 400 {
		return false
	}
	if sub2apiCapacityShed(body) {
		return true
	}
	if status != 400 {
		return false
	}
	match := func(s string) bool {
		s = strings.ToLower(s)
		return strings.Contains(s, "an error occurred while processing your request") || strings.Contains(s, "selected model is at capacity") || strings.Contains(s, "you can retry your request") && strings.Contains(s, "help.openai.com") && strings.Contains(s, "request id")
	}
	for _, path := range []string{"error.message", "response.error.message", "message"} {
		if match(gjson.Get(body, path).String()) {
			return true
		}
	}
	return !gjson.Valid(body) && match(body)
}
func sub2apiAccessState(body string) bool {
	for _, path := range []string{"error.code", "response.error.code", "detail.code", "code"} {
		code := strings.ToLower(strings.TrimSpace(gjson.Get(body, path).String()))
		for _, subject := range []string{"workspace", "account", "organization", "org"} {
			for _, state := range []string{"deactivated", "disabled", "suspended"} {
				if code == subject+"_"+state || code == state+"_"+subject {
					return true
				}
			}
		}
	}
	return false
}
func sub2apiMissingModel400(body string) bool {
	if code := sub2apiErrorField(body, "code"); code != "" {
		return strings.EqualFold(code, "model_not_found")
	}
	msg := strings.ToLower(sub2apiErrorField(body, "message"))
	if !gjson.Valid(body) {
		msg = strings.ToLower(body)
	}
	return strings.Contains(msg, "unknown provider for model") || strings.Contains(msg, "model not found") || strings.Contains(msg, "model is not supported")
}
func sub2apiPersistentTransport(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) && dns.IsNotFound {
		return true
	}
	for _, marker := range []string{"authentication failed", "proxy authentication required", "connection refused", "no route to host", "network is unreachable", "no such host"} {
		if strings.Contains(strings.ToLower(err.Error()), marker) {
			return true
		}
	}
	return false
}

type sub2apiStorm struct {
	mu    sync.Mutex
	start time.Time
	count int
}

func (s *sub2apiStorm) observe(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.start.IsZero() || now.Sub(s.start) >= 10*time.Second {
		s.start = now
		s.count = 1
	} else {
		s.count++
	}
}
func (s *sub2apiStorm) active(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.start.IsZero() && now.Sub(s.start) < 10*time.Second && s.count >= 20
}

func sub2apiWait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// A Forward invocation has its own Claude HTTP retry budget; same-account
// retries belong to the request, and do not consume the account-switch budget.
func (m *Manager) sub2apiAttempt(ctx context.Context, a *Auth, run func() error, bootstrap bool) error {
	s := sub2apiState(ctx)
	if s == nil {
		return run()
	}
	s.lastFailure = nil
	now, wait := s.now, s.wait
	if now == nil {
		now = time.Now
	}
	if wait == nil {
		wait = sub2apiWait
	}
	for {
		start := now()
		var err error
		for attempt := 1; ; attempt++ {
			if err = ctx.Err(); err != nil {
				return err
			}
			err = run()
			if err == nil {
				s.lastFailure = nil
				m.sub2apiOAuth429.Delete(a.ID)
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if isRequestTerminatedError(err) {
				return err
			}
			var authErr *Error
			if errors.As(err, &authErr) && authErr.Code == capacityExceededCode {
				return err
			}
			if isRequestScopedError(err) && !(a.Provider == "codex" && (sub2apiIsStreamFailure(err) || sub2apiStatus(err) == 413 && !sub2apiContextError(sub2apiErrorBody(err)) || sub2apiTransient(sub2apiStatus(err), sub2apiErrorBody(err)))) {
				return err
			}
			if errors.Is(err, context.Canceled) {
				return wrapRequestStopError(err)
			}
			if sub2apiIsStreamFailure(err) || !sub2apiClaudeRetry(a, sub2apiStatus(err)) || attempt >= 5 {
				break
			}
			remaining := 10*time.Second - now().Sub(start)
			if remaining <= 0 {
				break
			}
			delay := min(300*time.Millisecond*time.Duration(1<<(attempt-1)), 3*time.Second, remaining)
			if errWait := wait(ctx, delay); errWait != nil {
				return errWait
			}
		}
		failure := m.classifySub2APIFailure(a, err, bootstrap)
		headers := s.headers
		if headers == nil {
			headers = logging.GetResponseHeaders(ctx)
		}
		m.refineSub2APIFailure(a, failure, headers, s.model, now(), true)
		if failure.same && failure.retry && ((!failure.retryDeadline.IsZero() && now().Before(failure.retryDeadline)) || failure.retryDeadline.IsZero() && s.sameRetries[a.ID] < sub2apiSameLimit(a)) {
			s.sameRetries[a.ID]++
			delay := 500 * time.Millisecond
			if failure.capacity {
				for i := 1; i < s.sameRetries[a.ID]; i++ {
					delay = min(delay*2, 8*time.Second)
				}
			}
			if failure.retryDelay > 0 {
				delay = failure.retryDelay
			}
			if !failure.retryDeadline.IsZero() {
				delay = min(delay, max(0, failure.retryDeadline.Sub(now())))
			}
			if errWait := wait(ctx, delay); errWait != nil {
				return errWait
			}
			continue
		}
		if failure.same && a.Provider == "claude" && failure.status == 502 {
			failure.neutral = false
			failure.cooldown = time.Minute
		}
		if failure.retry {
			if s.switches >= s.maxSwitches || a.Provider == "codex" && a.AuthKind() == AuthKindOAuth && failure.status == 429 && s.switches >= 2 {
				failure.retry = false
			} else {
				s.switches++
			}
		}
		s.lastFailure = failure
		return failure
	}
}
