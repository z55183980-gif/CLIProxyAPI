package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestSub2APILatestOAuth429RetryWindow(t *testing.T) {
	m := NewManager(nil, nil, nil)
	a := &Auth{ID: "oauth", Provider: "codex", Attributes: map[string]string{"auth_kind": AuthKindOAuth}}
	ctx := m.withSub2APIFailover(context.Background(), []string{"codex"})
	s := sub2apiState(ctx)
	now := time.Unix(1800000000, 0)
	start := now
	s.now = func() time.Time { return now }
	s.headers = http.Header{"Retry-After": []string{"20"}, "X-Codex-Primary-Used-Percent": []string{"20"}, "X-Codex-Primary-Reset-After-Seconds": []string{"60000"}}
	s.wait = func(_ context.Context, d time.Duration) error {
		if d > 8*time.Second {
			t.Fatal(d)
		}
		now = now.Add(d)
		return nil
	}
	calls := 0
	f := sub2apiFailureFrom(m.sub2apiAttempt(ctx, a, func() error { calls++; return &Error{HTTPStatus: 429, Message: "temporary limit"} }, false))
	if calls != 16 || now.Sub(start) != 2*time.Minute || f.cooldown != 5*time.Second || f.neutral || !f.retry {
		t.Fatalf("calls=%d elapsed=%s failure=%+v", calls, now.Sub(start), f)
	}
	if _, ok := m.sub2apiOAuth429.Load(a.ID); ok {
		t.Fatal("expired window retained")
	}
}

func TestSub2APILatestOAuth429QuotaAndCancellation(t *testing.T) {
	for _, quota := range []bool{false, true} {
		m := NewManager(nil, nil, nil)
		a := &Auth{ID: "oauth", Provider: "codex", Attributes: map[string]string{"auth_kind": AuthKindOAuth}}
		ctx, cancel := context.WithCancel(context.Background())
		ctx = m.withSub2APIFailover(ctx, []string{"codex"})
		s := sub2apiState(ctx)
		waits := 0
		s.wait = func(context.Context, time.Duration) error { waits++; cancel(); return ctx.Err() }
		body := "limit"
		if quota {
			body = `{"error":{"type":"usage_limit_reached","resets_in_seconds":120}}`
		}
		err := m.sub2apiAttempt(ctx, a, func() error { return &Error{HTTPStatus: 429, Message: body} }, false)
		cancel()
		if quota {
			if waits != 0 || sub2apiFailureFrom(err).cooldown != 2*time.Minute {
				t.Fatal(err, waits)
			}
		} else if waits != 1 || !errors.Is(err, context.Canceled) {
			t.Fatal(err, waits)
		}
	}
}

func TestSub2APILatestFableCreditsScope(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, tc := range []struct {
		model, body string
		family      bool
	}{
		{"claude-fable-5-1", `{"error":{"details":{"error_code":"credits_required"}}}`, true},
		{"claude-sonnet-4", `{"error":{"details":{"error_code":"credits_required"}}}`, false},
		{"claude-fable-5-1", `{"error":{"details":{"error_code":"credits_required","model":"claude-sonnet-4"}}}`, false},
	} {
		m := NewManager(nil, nil, nil)
		a := &Auth{ID: "fable", Provider: "claude"}
		f := m.classifySub2APIFailure(a, &Error{HTTPStatus: 429, Message: tc.body}, false)
		m.refineSub2APIFailure(a, f, nil, tc.model, now, true)
		if f.modelOnly != tc.family || f.cooldown != 5*time.Second {
			t.Fatalf("model=%s failure=%+v", tc.model, f)
		}
	}
}

func TestSub2APILatestStructuredErrorsAndHTML403(t *testing.T) {
	m := NewManager(nil, nil, nil)
	a := &Auth{ID: "a", Provider: "codex"}
	for range 4 {
		f := m.classifySub2APIFailure(a, &Error{HTTPStatus: 403, Message: "<!DOCTYPE html><html>Forbidden</html>"}, false)
		if !f.retry || !f.neutral || f.permanent {
			t.Fatal(f)
		}
	}
	body := `{"error":{"message":"upstream failure"},"request":{"message":"context_length_exceeded server is overloaded"}}`
	if sub2apiContextError(body) || sub2apiCapacityShed(body) || sub2apiTransient(400, body) {
		t.Fatal("echoed input altered classification")
	}
	for _, tc := range []struct {
		status int
		body   string
	}{
		{405, `{"error":{"message":"method not allowed"}}`},
		{400, `{"error":{"code":"model_not_found"}}`},
		{502, `{"error":{"code":"workspace_disabled"}}`},
	} {
		if f := m.classifySub2APIFailure(a, &Error{HTTPStatus: tc.status, Message: tc.body}, false); !f.retry {
			t.Fatal(f)
		}
	}
}

func TestSub2APILatestCapacityBackoff(t *testing.T) {
	m := NewManager(nil, nil, nil)
	a := &Auth{ID: "a", Provider: "codex"}
	ctx := m.withSub2APIFailover(context.Background(), []string{"codex"})
	var delays []time.Duration
	sub2apiState(ctx).wait = func(_ context.Context, d time.Duration) error { delays = append(delays, d); return nil }
	f := sub2apiFailureFrom(m.sub2apiAttempt(ctx, a, func() error { return &Error{HTTPStatus: 429, Message: `{"error":{"code":"server_is_overloaded"}}`} }, false))
	if len(delays) != 3 || delays[0] != 500*time.Millisecond || delays[1] != time.Second || delays[2] != 2*time.Second || !f.neutral || f.status != 503 {
		t.Fatal(delays, f)
	}
}

func TestSub2APILatestTeamFailureIsolation(t *testing.T) {
	m := NewManager(nil, nil, nil)
	for _, id := range []string{"a", "b", "other"} {
		team := "team-one"
		if id == "other" {
			team = "team-two"
		}
		_, err := m.Register(context.Background(), &Auth{ID: id, Provider: "codex", Status: StatusActive, Attributes: map[string]string{"auth_kind": AuthKindOAuth}, Metadata: map[string]any{"account_id": team}})
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx := m.withSub2APIFailover(context.Background(), []string{"codex"})
	a, _ := m.GetByID("a")
	cause := &Error{HTTPStatus: 402, Message: `{"detail":{"code":"deactivated_workspace"}}`}
	sub2apiState(ctx).lastFailure = m.classifySub2APIFailure(a, cause, false)
	m.MarkResult(ctx, Result{AuthID: "a", Provider: "codex", Error: cause})
	for _, id := range []string{"a", "b", "other"} {
		a, _ := m.GetByID(id)
		if a.Disabled != (id != "other") {
			t.Fatalf("id=%s disabled=%t", id, a.Disabled)
		}
	}
}

func TestSub2APILatestStreamQuotaIgnoresHTTP200Snapshot(t *testing.T) {
	m := NewManager(nil, nil, nil)
	a := &Auth{ID: "oauth", Provider: "codex", Attributes: map[string]string{"auth_kind": AuthKindOAuth}}
	now := time.Unix(1800000000, 0)
	headers := http.Header{"X-Codex-Primary-Used-Percent": []string{"100"}, "X-Codex-Primary-Reset-After-Seconds": []string{"36000"}}
	cause := &sub2apiFailedEventError{&Error{HTTPStatus: 429, Message: `{"response":{"error":{"type":"rate_limit_error"}}}`}}
	f := m.classifySub2APIFailure(a, cause, true)
	m.refineSub2APIFailure(a, f, headers, "gpt-5.4", now, true)
	if !f.same || !f.neutral || f.cooldown != 0 {
		t.Fatal(f)
	}
	f = m.classifySub2APIFailure(a, cause, true)
	m.refineSub2APIFailure(a, f, headers, "gpt-5.3-codex-spark", now, true)
	if !f.modelOnly || f.cooldown != 10*time.Hour {
		t.Fatal(f)
	}
}

func TestSub2APILatestClaudeStreamOverloadPolicy(t *testing.T) {
	m := NewManager(nil, nil, nil)
	cause := &sub2apiFailedEventError{&Error{HTTPStatus: 502, Message: `{"type":"error","error":{"type":"overloaded_error"}}`}}
	for _, pool := range []bool{false, true} {
		a := &Auth{ID: "claude", Provider: "claude", Attributes: map[string]string{"auth_kind": AuthKindAPIKey}, Metadata: map[string]any{"pool_mode": pool}}
		f := m.classifySub2APIFailure(a, cause, true)
		if f.status != 529 || !f.retry || f.neutral != pool {
			t.Fatal(f)
		}
		if !pool && f.cooldown != 10*time.Minute {
			t.Fatal(f)
		}
		f = m.classifySub2APIFailure(a, cause, false)
		if f.status != 403 || !f.neutral {
			t.Fatal(f)
		}
	}
}

func TestSub2APILatestEmptyAddedEventsDoNotCommitOutput(t *testing.T) {
	for _, tc := range []struct {
		payload, kind string
		commits       bool
	}{
		{`{"item":{"type":"message","content":[]}}`, "response.output_item.added", false},
		{`{"item":{"type":"function_call","arguments":""}}`, "response.output_item.added", false},
		{`{"item":{"type":"reasoning","summary":[]}}`, "response.output_item.added", false},
		{`{"item":{"type":"reasoning","encrypted_content":"opaque"}}`, "response.output_item.added", true},
		{`{"part":{"type":"output_text","text":""}}`, "response.content_part.added", false},
		{`{"part":{"type":"output_text","text":"hello"}}`, "response.content_part.added", true},
		{`{"item":{"type":"function_call","arguments":"{}"}}`, "response.output_item.added", true},
	} {
		if got := sub2apiAddedEventStartsOutput([]byte(tc.payload), tc.kind); got != tc.commits {
			t.Fatalf("payload=%s commits=%t", tc.payload, got)
		}
	}
}
