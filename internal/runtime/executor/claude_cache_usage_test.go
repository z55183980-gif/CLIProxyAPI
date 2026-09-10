package executor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestClaudeExecutorStreamCacheTTL(t *testing.T) {
	for _, format := range []sdktranslator.Format{sdktranslator.FormatClaude, sdktranslator.FormatOpenAI} {
		t.Run(string(format), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: "+`{"type":"message_start","message":{"id":"ttl-test","type":"message","role":"assistant","model":"claude-sonnet-5","content":[],"usage":{"input_tokens":10,"output_tokens":1,"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":70}}}}`+"\n\n"+
					"data: "+`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`+"\n\n"+
					"data: "+`{"type":"message_stop"}`+"\n\n")
			}))
			defer server.Close()
			plugin := &captureAIStudioUsagePlugin{records: make(chan usage.Record, 16)}
			usage.RegisterPlugin(plugin)
			authID := "cache-ttl-" + string(format)
			e := NewClaudeExecutor(&config.Config{})
			result, err := e.ExecuteStream(context.Background(), &cliproxyauth.Auth{ID: authID, Provider: "claude", Attributes: map[string]string{"api_key": "test-key", "base_url": server.URL}}, cliproxyexecutor.Request{Model: "claude-sonnet-5", Payload: []byte(`{"model":"claude-sonnet-5","messages":[{"role":"user","content":"hi"}],"max_tokens":32}`)}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude, ResponseFormat: format})
			if err != nil {
				t.Fatal(err)
			}
			for chunk := range result.Chunks {
				if chunk.Err != nil {
					t.Fatal(chunk.Err)
				}
			}
			deadline := time.After(3 * time.Second)
			for {
				select {
				case r := <-plugin.records:
					if r.AuthID != authID {
						continue
					}
					if r.Failed || r.Detail.InputTokens != 10 || r.Detail.OutputTokens != 12 || r.Detail.CacheCreation5mTokens != 30 || r.Detail.CacheCreation1hTokens != 70 || r.Detail.TotalTokens != 122 {
						t.Fatalf("incomplete stream usage: %+v", r)
					}
					return
				case <-deadline:
					t.Fatal("missing usage record")
				}
			}
		})
	}
}
