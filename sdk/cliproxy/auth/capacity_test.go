package auth

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func TestCapacityAtomicAdmissionAndLiveLimit(t *testing.T) {
	m := NewManager(nil, nil, nil)
	a := &Auth{ID: "capacity", Metadata: map[string]any{"concurrency": 5}}
	if _, err := m.Register(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	releases := make(chan func(), 100)
	for range 100 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if release, err := m.acquireCapacity(a); err == nil {
				releases <- release
			}
		}()
	}
	workers.Wait()
	close(releases)
	if got := m.CurrentConcurrency(a.ID); got != 5 {
		t.Fatalf("active = %d, want 5", got)
	}
	a.Metadata["concurrency"] = 1
	if _, err := m.Update(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if release, err := m.acquireCapacity(a); err == nil {
		release()
		t.Fatal("lowered limit admitted an extra request")
	}
	for release := range releases {
		release()
		release()
	}
	if got := m.CurrentConcurrency(a.ID); got != 0 {
		t.Fatalf("active = %d after release", got)
	}
	release, err := m.acquireCapacity(a)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestCapacityStreamLifetimeAndFallback(t *testing.T) {
	source := make(chan cliproxyexecutor.StreamChunk, 1)
	source <- cliproxyexecutor.StreamChunk{Payload: []byte("first")}
	executor := &claudeCancellationTestExecutor{streamFn: func(context.Context, *Auth) (*cliproxyexecutor.StreamResult, error) {
		return &cliproxyexecutor.StreamResult{Chunks: source}, nil
	}}
	m, a, model := newClaudeCancellationTestManager(t, executor, nil)
	a.Metadata["concurrency"] = 1
	a.Attributes["priority"] = "1"
	if _, err := m.Update(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	b := &Auth{ID: a.ID + "-fallback", Provider: "claude", Attributes: map[string]string{"priority": "2"}}
	if _, err := m.Register(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	registry.GetGlobalRegistry().RegisterClient(b.ID, b.Provider, []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(b.ID) })
	executor.executeFn = func(_ context.Context, selected *Auth) (cliproxyexecutor.Response, error) {
		return cliproxyexecutor.Response{Payload: []byte(selected.ID)}, nil
	}
	stream, err := m.ExecuteStream(context.Background(), []string{"claude"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	<-stream.Chunks
	if got := m.CurrentConcurrency(a.ID); got != 1 {
		t.Fatalf("stream active = %d", got)
	}
	resp, err := m.Execute(context.Background(), []string{"claude"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if err != nil || string(resp.Payload) != b.ID {
		t.Fatalf("fallback = %s, %v", resp.Payload, err)
	}
	current, _ := m.GetByID(a.ID)
	if current.Unavailable || current.Failed != 0 {
		t.Fatalf("local capacity changed credential health: %#v", current)
	}
	close(source)
	for range stream.Chunks {
	}
	if got := m.CurrentConcurrency(a.ID); got != 0 {
		t.Fatalf("finished stream active = %d", got)
	}
	resp, err = m.Execute(context.Background(), []string{"claude"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if err != nil || string(resp.Payload) != a.ID {
		t.Fatalf("recovered priority = %s, %v", resp.Payload, err)
	}
}

func TestParseCapacity(t *testing.T) {
	for _, value := range []any{-1, 1.5, "", "1.5", json.Number("1.5"), 1000001, true} {
		if _, err := ParseCapacity(value); err == nil {
			t.Fatalf("accepted %#v", value)
		}
	}
	for _, value := range []any{nil, 0, 5, float64(5), json.Number("5"), "5"} {
		if _, err := ParseCapacity(value); err != nil {
			t.Fatalf("rejected %#v: %v", value, err)
		}
	}
}

func TestCapacityReleasesOnExecutorErrorsAndCancellation(t *testing.T) {
	m := NewManager(nil, nil, nil)
	a := &Auth{ID: "capacity-errors", Metadata: map[string]any{"concurrency": 1}}
	failure := errors.New("upstream failed")
	executor := &claudeCancellationTestExecutor{
		executeFn: func(context.Context, *Auth) (cliproxyexecutor.Response, error) {
			return cliproxyexecutor.Response{}, failure
		},
		countFn: func(context.Context, *Auth) (cliproxyexecutor.Response, error) {
			return cliproxyexecutor.Response{}, failure
		},
		streamFn: func(context.Context, *Auth) (*cliproxyexecutor.StreamResult, error) { return nil, failure },
	}
	for _, count := range []bool{false, true} {
		if _, err := m.executeWithCapacity(context.Background(), executor, a, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}, count); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if m.CurrentConcurrency(a.ID) != 0 {
			t.Fatal("failed execution leaked capacity")
		}
	}
	if _, err := m.streamWithCapacity(context.Background(), executor, a, cliproxyexecutor.Request{}, cliproxyexecutor.Options{}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if m.CurrentConcurrency(a.ID) != 0 {
		t.Fatal("failed stream leaked capacity")
	}
	source := make(chan cliproxyexecutor.StreamChunk)
	executor.streamFn = func(context.Context, *Auth) (*cliproxyexecutor.StreamResult, error) {
		return &cliproxyexecutor.StreamResult{Chunks: source}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := m.streamWithCapacity(ctx, executor, a, cliproxyexecutor.Request{}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	close(source)
	for range stream.Chunks {
	}
	if m.CurrentConcurrency(a.ID) != 0 {
		t.Fatal("cancelled stream leaked capacity")
	}
}
