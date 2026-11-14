package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// A Claude OAuth stream the client abandons never reaches MarkResult, so its
// quota headers must be recorded as soon as the response head is accepted.
func TestExecuteStreamObservesQuotaBeforeClientCancels(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	model := "stream-observe-model"
	authID := "stream-observe-claude"
	registry.GetGlobalRegistry().RegisterClient(authID, "claude", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(authID) })
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: authID, Provider: "claude", Status: StatusActive, Attributes: map[string]string{"auth_kind": "oauth"}}); errRegister != nil {
		t.Fatal(errRegister)
	}
	upstreamDone := make(chan struct{})
	manager.RegisterExecutor(&customStreamMockExecutor{
		identifier: "claude",
		streamFn: func(ctx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
			internallogging.SetResponseHeaders(ctx, http.Header{
				"Anthropic-Ratelimit-Unified-5h-Utilization": []string{"0.42"},
				"Anthropic-Ratelimit-Unified-5h-Status":      []string{"allowed"},
			})
			chunks := make(chan cliproxyexecutor.StreamChunk, 1)
			chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("data: {}\n\n")}
			go func() {
				<-ctx.Done()
				close(chunks)
				close(upstreamDone)
			}()
			return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, errStream := manager.ExecuteStream(ctx, []string{"claude"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if errStream != nil {
		t.Fatal(errStream)
	}
	if chunk, ok := <-stream.Chunks; !ok || chunk.Err != nil {
		t.Fatalf("first chunk = %+v ok=%v", chunk, ok)
	}

	observed := func() QuotaState {
		live, _ := manager.GetByID(authID)
		return live.Quota
	}
	if got := observed(); got.Signals["Anthropic-Ratelimit-Unified-5h-Utilization"] != "0.42" || got.ObservedAt.IsZero() {
		t.Fatalf("quota not observed while the stream is still open: %#v", got)
	}

	cancel()
	select {
	case <-upstreamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream stream was not released")
	}
	for range stream.Chunks {
	}
	if live, _ := manager.GetByID(authID); live.Quota.Signals["Anthropic-Ratelimit-Unified-5h-Utilization"] != "0.42" || live.Success != 0 || live.Failed != 0 {
		t.Fatalf("abandoned stream changed accounting or lost the observation: quota=%#v success=%d failed=%d", live.Quota, live.Success, live.Failed)
	}
}
