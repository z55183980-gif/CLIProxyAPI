package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// Exercise the actual selector, auth cloning, Claude request preparation,
// HTTP transport and response readers with one account. The upstream gate
// cannot open until every request has arrived: a per-account execution lock
// or a small connection cap fails deterministically instead of hiding in timing.
func TestClaudeSingleAccountConcurrentHTTPAndRecovery(t *testing.T) {
	const parallel = 64
	const model = "claude-sonnet-4-6"
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", streaming), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			entered := make(chan int, parallel*2+1)
			departed := make(chan struct{}, parallel*2+1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var active, peak atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read upstream body: %v", err)
					return
				}
				if r.URL.Path != "/v1/messages" || gjson.GetBytes(body, "model").String() != model {
					t.Errorf("unexpected upstream request: %s %s", r.URL.Path, body)
				}
				if r.Header.Get("Authorization") != "Bearer sk-ant-oat-local-concurrency" {
					t.Errorf("request did not use the single test credential")
				}
				phase := int(gjson.GetBytes(body, "max_tokens").Int())
				if phase == 33 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"test request rejected"}}`)
					return
				}
				n := active.Add(1)
				defer func() { active.Add(-1); departed <- struct{}{} }()
				for old := peak.Load(); n > old; old = peak.Load() {
					if peak.CompareAndSwap(old, n) {
						break
					}
				}
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_parallel\",\"type\":\"message\",\"model\":\""+model+"\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
					w.(http.Flusher).Flush()
				}
				entered <- phase
				select {
				case <-r.Context().Done():
					return
				case <-release:
				}
				if streaming {
					_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(w, `{"id":"msg_parallel","type":"message","model":%q,"role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`, model)
				}
			}))
			defer server.Close()
			defer unblock()
			defer cancel()
			auth := &cliproxyauth.Auth{
				ID: "claude-concurrent-" + uuid.NewString(), Provider: "claude", Status: cliproxyauth.StatusActive,
				Attributes: map[string]string{"auth_kind": "oauth", "base_url": server.URL},
				Metadata:   claudeOAuthTestMetadata(),
			}
			auth.Metadata["access_token"] = "sk-ant-oat-local-concurrency"
			manager := cliproxyauth.NewManager(nil, nil, nil)
			manager.SetRetryConfig(0, 0, 0)
			manager.RegisterExecutor(NewClaudeExecutor(&config.Config{}))
			registry.GetGlobalRegistry().RegisterClient(auth.ID, "claude", []*registry.ModelInfo{{ID: model}})
			defer registry.GetGlobalRegistry().UnregisterClient(auth.ID)
			if _, err := manager.Register(ctx, auth); err != nil {
				t.Fatal(err)
			}
			run := func(requestCtx context.Context, phase int) error {
				payload := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":%d,"messages":[{"role":"user","content":"hello"}]}`, model, phase))
				req := cliproxyexecutor.Request{Model: model, Payload: payload}
				opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude, OriginalRequest: payload, Stream: streaming}
				var result strings.Builder
				if streaming {
					stream, err := manager.ExecuteStream(requestCtx, []string{"claude"}, req, opts)
					if err != nil {
						return err
					}
					for chunk := range stream.Chunks {
						if chunk.Err != nil {
							return chunk.Err
						}
						result.Write(chunk.Payload)
					}
				} else {
					response, err := manager.Execute(requestCtx, []string{"claude"}, req, opts)
					if err != nil {
						return err
					}
					result.Write(response.Payload)
				}
				if err := requestCtx.Err(); err != nil {
					return err
				}
				if !strings.Contains(result.String(), `"ok"`) {
					return fmt.Errorf("missing response content: %s", result.String())
				}
				return nil
			}
			startBatch := func(batchCtx context.Context, phase int) <-chan error {
				results := make(chan error, parallel)
				for i := 0; i < parallel; i++ {
					go func() { results <- run(batchCtx, phase) }()
				}
				for i := 0; i < parallel; i++ {
					select {
					case actualPhase := <-entered:
						if actualPhase != phase {
							t.Fatalf("upstream phase=%d, want %d", actualPhase, phase)
						}
					case err := <-results:
						t.Fatalf("request ended before all %d arrived: %v", parallel, err)
					case <-ctx.Done():
						t.Fatalf("only %d/%d reached upstream concurrently: %v", i, parallel, ctx.Err())
					}
				}
				if got := active.Load(); got != parallel {
					t.Fatalf("active=%d, want %d", got, parallel)
				}
				return results
			}
			waitBatch := func(results <-chan error, canceled bool) {
				for i := 0; i < parallel; i++ {
					select {
					case err := <-results:
						if canceled && !errors.Is(err, context.Canceled) {
							t.Errorf("canceled request=%v", err)
						}
						if !canceled && err != nil {
							t.Errorf("recovery request=%v", err)
						}
					case <-ctx.Done():
						t.Fatal("request did not terminate")
					}
				}
				for i := 0; i < parallel; i++ {
					select {
					case <-departed:
					case <-ctx.Done():
						t.Fatal("upstream request did not release")
					}
				}
			}
			canceledCtx, cancelBatch := context.WithCancel(ctx)
			defer cancelBatch()
			canceled := startBatch(canceledCtx, 32)
			cancelBatch()
			waitBatch(canceled, true)
			if err := run(ctx, 33); err == nil || !strings.Contains(err.Error(), "test request rejected") {
				t.Fatalf("upstream error not propagated: %v", err)
			}
			recovered := startBatch(ctx, 34)
			unblock()
			waitBatch(recovered, false)
			t.Logf("single Claude account: %d simultaneous upstream requests; %d canceled and %d successful after request error", peak.Load(), parallel, parallel)
		})
	}
}
