package helps

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type reporterQueuePlugin func(context.Context, usage.Record)

func (f reporterQueuePlugin) HandleUsage(ctx context.Context, record usage.Record) {
	f(ctx, record)
}

func TestUsageReporterCancelledRequestWaitsForFullQueue(t *testing.T) {
	m := usage.DefaultManager()
	shutdown := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			t.Fatalf("drain usage manager: %v", err)
		}
	}
	shutdown()
	m.Start(context.Background())
	const pluginName = "test:reporter-queue-backpressure"
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() {
		unblock()
		shutdown()
		m.RegisterNamed(pluginName, reporterQueuePlugin(func(context.Context, usage.Record) {}))
		m.Start(context.Background())
	}()
	var count int
	var accounted usage.Record
	var handlerErr error
	var requestID string
	m.RegisterNamed(pluginName, reporterQueuePlugin(func(ctx context.Context, record usage.Record) {
		if record.RequestID == "blocker" {
			close(entered)
			<-release
		}
		if record.RequestID == "completed-claude-request" {
			count++
			accounted, handlerErr = record, ctx.Err()
			requestID = internallogging.GetRequestID(ctx)
		}
	}))
	if err := m.PublishContext(context.Background(), usage.Record{RequestID: "blocker"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("usage plugin did not start")
	}
	// A cancelled enqueue context still accepts an available slot; keep
	// publishing until it reports that the real default queue is saturated.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	full := false
	for i := 0; i < 4096; i++ {
		err := m.PublishContext(cancelled, usage.Record{RequestID: "filler"})
		if errors.Is(err, context.Canceled) {
			full = true
			break
		}
		if err != nil {
			t.Fatalf("fill usage queue: %v", err)
		}
	}
	if !full {
		t.Fatal("usage queue did not apply its capacity limit")
	}
	ctx := internallogging.WithRequestID(cancelled, "completed-claude-request")
	ctx = internallogging.WithResponseHeadersHolder(ctx)
	internallogging.SetResponseHeaders(ctx, http.Header{"Request-Id": {"upstream-id"}})
	reporter := NewUsageReporter(ctx, "claude", "claude-sonnet-4-6", nil)
	finished := make(chan struct{})
	go func() {
		reporter.Publish(ctx, usage.Detail{InputTokens: 10, OutputTokens: 5, TotalTokens: 15})
		// The once guard must still prevent a duplicate after queue acceptance.
		reporter.EnsurePublished(ctx)
		close(finished)
	}()
	select {
	case <-finished:
		t.Fatal("cancelled request dropped its accounting instead of waiting for queue capacity")
	case <-time.After(25 * time.Millisecond):
	}
	unblock()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("reporter did not resume after queue capacity became available")
	}
	shutdown()
	if count != 1 || accounted.Detail.InputTokens != 10 || accounted.Detail.OutputTokens != 5 || accounted.Detail.TotalTokens != 15 {
		t.Fatalf("accounted count=%d record=%+v, want one completed 15-token request", count, accounted)
	}
	if handlerErr != nil || requestID != "completed-claude-request" || accounted.ResponseHeaders.Get("Request-Id") != "upstream-id" {
		t.Fatalf("accounting lost context metadata: err=%v requestID=%q headers=%v", handlerErr, requestID, accounted.ResponseHeaders)
	}
}
