package test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestSub2APICodexPreservesOriginalHTTPFailure(t *testing.T) {
	for _, tc := range []struct {
		name               string
		status             int
		body               string
		failover, disabled bool
	}{
		{"body-too-large", 413, `{"error":{"message":"request body too large"}}`, true, false},
		{"context", 413, `{"error":{"code":"context_length_exceeded"}}`, false, false},
		{"revoked", 401, `{"error":{"code":"token_revoked"}}`, true, true},
		{"transient", 400, `{"error":{"code":"slow_down"}}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := make(chan string, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				calls <- key
				if key == "primary" {
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprint(w, tc.body)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"ok\",\"status\":\"completed\",\"output\":[]}}\n\n")
			}))
			defer server.Close()
			m := cliproxyauth.NewManager(nil, &cliproxyauth.RoundRobinSelector{}, nil)
			m.RegisterExecutor(runtimeexecutor.NewCodexExecutor(&config.Config{}))
			for i, key := range []string{"primary", "backup"} {
				id := tc.name + key
				registry.GetGlobalRegistry().RegisterClient(id, "codex", []*registry.ModelInfo{{ID: "gpt-5.4"}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
				_, err := m.Register(context.Background(), &cliproxyauth.Auth{ID: id, Provider: "codex", Status: cliproxyauth.StatusActive,
					Attributes: map[string]string{"auth_kind": cliproxyauth.AuthKindOAuth, "base_url": server.URL, "priority": fmt.Sprint(i + 1)},
					Metadata:   map[string]any{"access_token": key, "refresh_token": "refresh"},
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := m.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: "gpt-5.4", Payload: []byte(`{"model":"gpt-5.4","input":"test"}`)}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai-response")})
			if (err == nil) != tc.failover {
				t.Fatalf("failover=%t err=%v", tc.failover, err)
			}
			want := 1
			if tc.failover {
				want = 2
				if tc.name == "transient" {
					want = 5
				}
			}
			if len(calls) != want {
				t.Fatalf("calls=%d want=%d", len(calls), want)
			}
			a, _ := m.GetByID(tc.name + "primary")
			if a.Disabled != tc.disabled {
				t.Fatalf("disabled=%t want=%t", a.Disabled, tc.disabled)
			}
		})
	}
}
