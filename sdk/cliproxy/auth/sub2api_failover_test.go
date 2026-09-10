package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func TestSub2APIClaudeHTTPRetries(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		status     int
		metadata   map[string]any
		want       int
	}{
		{"oauth403", AuthKindOAuth, 403, nil, 5},
		{"oauth401", AuthKindOAuth, 401, nil, 1},
		{"oauth500", AuthKindOAuth, 500, nil, 1},
		{"apikey403", AuthKindAPIKey, 403, nil, 1},
		{"customExcluded", AuthKindAPIKey, 503, map[string]any{"custom_error_codes_enabled": true, "custom_error_codes": []any{float64(401)}}, 5},
		{"custom400", AuthKindAPIKey, 400, map[string]any{"custom_error_codes_enabled": true, "custom_error_codes": []any{float64(401)}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(nil, nil, nil)
			ctx := m.withSub2APIFailover(context.Background(), []string{"claude"})
			s := sub2apiState(ctx)
			now := time.Unix(100, 0)
			var delays []time.Duration
			s.now = func() time.Time { return now }
			s.wait = func(_ context.Context, d time.Duration) error {
				delays = append(delays, d)
				now = now.Add(d)
				return nil
			}
			a := &Auth{ID: tc.name, Provider: "claude", Attributes: map[string]string{"auth_kind": tc.kind}, Metadata: tc.metadata}
			calls := 0
			err := m.sub2apiAttempt(ctx, a, func() error { calls++; return &Error{HTTPStatus: tc.status, Message: "upstream"} }, false)
			if err == nil || calls != tc.want {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			if tc.want == 5 && !reflect.DeepEqual(delays, []time.Duration{300 * time.Millisecond, 600 * time.Millisecond, 1200 * time.Millisecond, 2400 * time.Millisecond}) {
				t.Fatalf("delays=%v", delays)
			}
		})
	}
}

func TestSub2APISameAccountBudgetAndCancellation(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		m := NewManager(nil, nil, nil)
		ctx := m.withSub2APIFailover(context.Background(), []string{provider})
		s := sub2apiState(ctx)
		s.wait = func(_ context.Context, d time.Duration) error {
			if d != 500*time.Millisecond {
				t.Fatalf("delay=%v", d)
			}
			return nil
		}
		a := &Auth{ID: provider, Provider: provider, Attributes: map[string]string{"auth_kind": AuthKindAPIKey}, Metadata: map[string]any{"pool_mode": true}}
		calls := 0
		failure := sub2apiFailureFrom(m.sub2apiAttempt(ctx, a, func() error { calls++; return &Error{HTTPStatus: 429, Message: "limited"} }, false))
		if calls != 4 || failure == nil || !failure.retry || !failure.neutral || s.switches != 1 {
			t.Fatalf("%s calls=%d failure=%+v switches=%d", provider, calls, failure, s.switches)
		}
		a.Metadata["pool_mode_retry_status_codes"] = []any{}
		if sub2apiPoolStatus(a, 429) {
			t.Fatal("explicit empty list must disable status retries")
		}
	}
	m := NewManager(nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	ctx = m.withSub2APIFailover(ctx, []string{"claude"})
	sub2apiState(ctx).wait = func(context.Context, time.Duration) error { cancel(); return ctx.Err() }
	a := &Auth{ID: "cancel", Provider: "claude", Attributes: map[string]string{"auth_kind": AuthKindOAuth}}
	calls := 0
	err := m.sub2apiAttempt(ctx, a, func() error { calls++; return &Error{HTTPStatus: 403} }, false)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestSub2APIProviderFailureMatrix(t *testing.T) {
	m := NewManager(nil, nil, nil)
	for _, tc := range []struct {
		provider string
		status   int
		body     string
		retry    bool
	}{
		{"claude", 400, "bad request", false}, {"claude", 402, "balance", true}, {"claude", 408, "timeout", false},
		{"claude", 404, "route not found", false}, {"claude", 404, "model not found", true}, {"claude", 503, "unavailable", true},
		{"codex", 400, "an error occurred while processing your request", true},
		{"codex", 400, `{"error":{"code":"slow_down"}}`, true},
		{"codex", 413, "large body", true}, {"codex", 413, "context_length_exceeded", false},
		{"codex", 503, "maximum context length", false}, {"codex", 408, "timeout", false},
		{"codex", 403, `{"error":{"code":"cyber_policy"}}`, false},
		{"codex", 404, "route not found", false},
	} {
		a := &Auth{ID: tc.provider, Provider: tc.provider, Attributes: map[string]string{"auth_kind": AuthKindOAuth}, Metadata: map[string]any{"refresh_token": "test"}}
		f := m.classifySub2APIFailure(a, &Error{HTTPStatus: tc.status, Message: tc.body}, false)
		if f.retry != tc.retry {
			t.Errorf("%s %d %q retry=%t", tc.provider, tc.status, tc.body, f.retry)
		}
	}
	a := &Auth{ID: "claude", Provider: "claude"}
	f := m.classifySub2APIFailure(a, io.ErrUnexpectedEOF, false)
	if !f.retry {
		t.Fatal("Claude transport errors must fail over")
	}
	f = m.classifySub2APIFailure(a, &sub2apiStreamReadError{error: io.ErrUnexpectedEOF}, true)
	if !f.retry || !f.same {
		t.Fatal("Claude pre-output read failure must retry same account")
	}
}

func TestSub2APIMaxSwitchesAndStorm(t *testing.T) {
	for _, storm := range []bool{false, true} {
		m := NewManager(nil, nil, nil)
		ctx := m.withSub2APIFailover(context.Background(), []string{"codex"})
		now := time.Now()
		if storm {
			for range 20 {
				m.sub2apiStorm.observe(now)
			}
		}
		want := 11
		if storm {
			want = 3
		}
		for attempt := 0; attempt < want; attempt++ {
			a := &Auth{ID: fmt.Sprint(attempt), Provider: "codex", Attributes: map[string]string{"auth_kind": AuthKindOAuth}}
			status := 503
			if storm {
				status = 429
			}
			f := sub2apiFailureFrom(m.sub2apiAttempt(ctx, a, func() error {
				return &Error{HTTPStatus: status, Message: `{"error":{"type":"usage_limit_reached","resets_in_seconds":30}}`}
			}, false))
			if f.retry != (attempt < want-1) {
				t.Fatalf("storm=%t attempt=%d retry=%t", storm, attempt, f.retry)
			}
		}
		if storm && m.sub2apiStorm.active(now.Add(10*time.Second)) {
			t.Fatal("expired storm remains active")
		}
	}
}

func TestSub2APIExecuteCodex401UsesBackupWithoutRefresh(t *testing.T) {
	for _, stream := range []bool{false, true} {
		m, executor, primary, backup, model := newUnauthorizedRefreshFixture(t, false)
		// The fixture covers the legacy provider; bind it to the Codex flow here.
		executor.id = "codex"
		m.RegisterExecutor(executor)
		for _, a := range []*Auth{primary, backup} {
			a.Provider = "codex"
			if _, err := m.Update(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			registry.GetGlobalRegistry().RegisterClient(a.ID, "codex", []*registry.ModelInfo{{ID: model}})
		}
		var payload []byte
		if stream {
			result, err := m.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
			if err != nil {
				t.Fatal(err)
			}
			for chunk := range result.Chunks {
				if chunk.Err != nil {
					t.Fatal(chunk.Err)
				}
				payload = append(payload, chunk.Payload...)
			}
		} else {
			result, err := m.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
			if err != nil {
				t.Fatal(err)
			}
			payload = result.Payload
		}
		if string(payload) != backup.ID+":backup-access-token" || executor.RefreshCalls() != 0 {
			t.Fatalf("payload=%q refresh=%d", payload, executor.RefreshCalls())
		}
		current, _ := m.GetByID(primary.ID)
		if !m.shouldRefresh(current, time.Now()) {
			t.Fatal("401 must remain eligible for background refresh")
		}
		if current.Disabled || !current.Unavailable || time.Until(current.NextRetryAfter) < 9*time.Minute {
			t.Fatalf("401 state=%+v", current)
		}
	}
}

func TestSub2APICodexPreambleFailoverAndOutputBoundary(t *testing.T) {
	for _, preamble := range []bool{true, false} {
		m := NewManager(nil, nil, nil)
		ids := registerOverloadAuths(t, m, 2)
		calls := 0
		m.RegisterExecutor(&customStreamMockExecutor{identifier: "codex", streamFn: func(_ context.Context, a *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
			calls++
			ch := make(chan cliproxyexecutor.StreamChunk, 2)
			if a.ID == ids[0] {
				typ := "response.output_text.delta"
				if preamble {
					typ = "response.created"
				}
				ch <- cliproxyexecutor.StreamChunk{Payload: []byte("data: {\"type\":\"" + typ + "\"}\n\n")}
				ch <- cliproxyexecutor.StreamChunk{Err: overloadStatusError()}
			} else {
				ch <- cliproxyexecutor.StreamChunk{Payload: []byte("data: {\"type\":\"response.completed\"}\n\n")}
			}
			close(ch)
			return &cliproxyexecutor.StreamResult{Chunks: ch}, nil
		}})
		result, err := m.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: "gpt-5.6-terra"}, cliproxyexecutor.Options{})
		if err != nil {
			t.Fatal(err)
		}
		failures := 0
		for chunk := range result.Chunks {
			if chunk.Err != nil {
				failures++
			}
		}
		if preamble && (calls != 5 || failures != 0) || !preamble && (calls != 1 || failures != 1) {
			t.Fatalf("preamble=%t calls=%d failures=%d", preamble, calls, failures)
		}
	}
}

func TestSub2APIQuotaWindowsAndTransientState(t *testing.T) {
	now := time.Unix(1800000000, 0)
	h := http.Header{}
	h.Set("anthropic-ratelimit-unified-5h-reset", fmt.Sprint(now.Add(time.Hour).Unix()))
	h.Set("anthropic-ratelimit-unified-7d-reset", fmt.Sprint(now.Add(24*time.Hour).Unix()))
	h.Set("anthropic-ratelimit-unified-5h-utilization", "1")
	if d, model := sub2api429Cooldown("claude", h, "", now); d != time.Hour || model {
		t.Fatalf("Claude cooldown=%v model=%t", d, model)
	}
	h.Set("anthropic-ratelimit-unified-7d-utilization", "1")
	if d, _ := sub2api429Cooldown("claude", h, "", now); d != 24*time.Hour {
		t.Fatal(d)
	}
	if d, _ := sub2api429Cooldown("codex", nil, "", now); d != 5*time.Second {
		t.Fatal(d)
	}
	var transient sub2apiTransientState
	for _, want := range []time.Duration{0, 10 * time.Second, 45 * time.Second} {
		if got := transient.record("a", "model", false, now); got != want {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
	transient.record("a", "model", true, now)
	if got := transient.record("a", "model", false, now); got != 0 {
		t.Fatal(got)
	}
	if got := transient.record("a", "model", false, now.Add(31*time.Minute)); got != 0 {
		t.Fatal(got)
	}
}

func TestSub2APICustomSwitchBudget(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.SetConfig(&internalconfig.Config{MaxAccountSwitches: 1})
	registerOverloadAuths(t, m, 3)
	calls := 0
	m.RegisterExecutor(&customStreamMockExecutor{identifier: "codex", streamFn: func(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
		calls++
		return nil, overloadStatusError()
	}})
	_, err := m.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: "gpt-5.6-terra"}, cliproxyexecutor.Options{})
	if statusCodeFromError(err) != 503 || calls != 8 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestSub2APIRefreshKeepsAccountPauseWithoutAnother401Refresh(t *testing.T) {
	now := time.Now()
	current := &Auth{ID: "refresh", Provider: "codex", Unavailable: true, Status: StatusError,
		NextRetryAfter: now.Add(10 * time.Minute),
		LastError:      &Error{Code: "sub2api_account_block", HTTPStatus: 401, Message: "expired token"},
		Metadata:       map[string]any{"refresh_token": "test"},
		ModelStates:    map[string]*ModelState{"old-model": {Status: StatusActive}},
	}
	updated := current.Clone()
	updated.LastError = nil
	updated.Status = StatusActive
	updated.Metadata["access_token"] = "refreshed"
	merged := MergeRefreshedAuth(current.Clone(), current, updated)
	if sub2apiRefreshPending(merged) {
		t.Fatal("successful refresh must not schedule another 401 refresh")
	}
	if blocked, _, until := isAuthBlockedForModel(merged, "new-model", now); !blocked || !until.Equal(current.NextRetryAfter) {
		t.Fatalf("refresh lost account pause: blocked=%t until=%s", blocked, until)
	}
	if blocked, _, _ := isAuthBlockedForModel(merged, "new-model", now.Add(10*time.Minute)); blocked {
		t.Fatal("expired account pause must permit selection")
	}
}

func TestSub2APIStreamFailedEventPolicy(t *testing.T) {
	for _, tc := range []struct {
		body  string
		retry bool
	}{
		{`{"response":{"error":{"type":"rate_limit_error"}}}`, true},
		{`{"response":{"error":{"type":"invalid_request_error"}}}`, false},
		{`{"response":{"error":{"message":"Rejected by safety policy"}}}`, false},
		{`{"response":{"error":{"message":"Not allowed"}}}`, false},
		{`{"response":{"error":{"code":"context_length_exceeded"}}}`, false},
		{`{"response":{"error":{"code":"slow_down","type":"invalid_request_error"}}}`, true},
	} {
		m := NewManager(nil, nil, nil)
		a := &Auth{ID: "stream", Provider: "codex"}
		failure := m.classifySub2APIFailure(a, &sub2apiFailedEventError{&Error{HTTPStatus: 429, Message: tc.body}}, true)
		wantStatus, wantNeutral, wantSame := 502, true, false
		if strings.Contains(tc.body, "rate_limit_error") {
			wantStatus, wantNeutral = 429, false
		}
		if strings.Contains(tc.body, "slow_down") {
			wantStatus, wantSame = 503, true
		}
		if failure.retry != tc.retry || failure.neutral != wantNeutral || failure.status != wantStatus || failure.same != wantSame {
			t.Fatalf("body=%s failure=%+v", tc.body, failure)
		}
	}
}

func TestSub2APIFableWindowBlocksFamilyOnly(t *testing.T) {
	now := time.Now()
	m := NewManager(nil, nil, nil)
	a := &Auth{ID: "fable", Provider: "claude"}
	f := &sub2apiFailure{status: 429, modelOnly: true, cooldown: time.Hour}
	m.applySub2APIResultLocked(a, Result{Model: "claude-fable-5[1m]", Error: &Error{HTTPStatus: 429}}, f, now)
	for _, model := range []string{"claude-fable-5", "claude-fable-5[1m]", "claude-sonnet-4"} {
		blocked, _, _ := isAuthBlockedForModel(a, model, now)
		if blocked != (model != "claude-sonnet-4") {
			t.Fatalf("model=%s blocked=%t", model, blocked)
		}
	}
}
