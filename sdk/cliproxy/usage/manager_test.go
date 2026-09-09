package usage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestStreamFromContextDefaultsMissingToFalse(t *testing.T) {
	if StreamFromContext(context.Background()) {
		t.Fatalf("StreamFromContext(background) = true, want false")
	}
}

type managerTestPlugin func(context.Context, Record)

func (f managerTestPlugin) HandleUsage(ctx context.Context, record Record) { f(ctx, record) }

func shutdownTestManager(t *testing.T, m *Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestManagerBoundedQueueCancellationAndDrain(t *testing.T) {
	m := NewManager(1)
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() { unblock(); shutdownTestManager(t, m) }()
	var got []string
	m.Register(managerTestPlugin(func(_ context.Context, record Record) {
		if record.RequestID == "first" {
			close(entered)
			<-release
		}
		got = append(got, record.RequestID)
	}))
	if err := m.PublishContext(context.Background(), Record{RequestID: "first"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("plugin did not start")
	}
	if err := m.PublishContext(context.Background(), Record{RequestID: "second"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := m.PublishContext(ctx, Record{RequestID: "rejected"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("full queue PublishContext = %v, want deadline exceeded", err)
	}
	m.mu.Lock()
	queued := m.running.size
	m.mu.Unlock()
	if queued != 1 {
		t.Fatalf("queued = %d, want capacity 1", queued)
	}
	if err := m.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown while plugin blocked = %v, want deadline exceeded", err)
	}
	if err := m.PublishContext(context.Background(), Record{}); !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("PublishContext during drain = %v, want ErrManagerStopped", err)
	}
	unblock()
	shutdownTestManager(t, m)
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("delivered = %v, want [first second]", got)
	}
}

func TestManagerStopUnblocksFullQueuePublisher(t *testing.T) {
	m := NewManager(1)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); shutdownTestManager(t, m) }()
	m.Register(managerTestPlugin(func(_ context.Context, r Record) {
		if r.RequestID == "first" {
			close(entered)
			<-release
		}
	}))
	m.Publish(context.Background(), Record{RequestID: "first"})
	<-entered
	m.Publish(context.Background(), Record{RequestID: "second"})
	result := make(chan error, 1)
	go func() { result <- m.PublishContext(context.Background(), Record{RequestID: "third"}) }()
	m.Stop()
	select {
	case err := <-result:
		if !errors.Is(err, ErrManagerStopped) {
			t.Fatalf("blocked PublishContext = %v, want ErrManagerStopped", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not wake publisher")
	}
}

func TestManagerAcceptedRecordSurvivesRequestCancellationAndMutation(t *testing.T) {
	m := NewManager(1)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); shutdownTestManager(t, m) }()
	type key struct{}
	var got Record
	var gotErr error
	var gotValue any
	m.Register(managerTestPlugin(func(ctx context.Context, r Record) {
		if r.RequestID == "blocker" {
			close(entered)
			<-release
			return
		}
		got, gotErr, gotValue = r, ctx.Err(), ctx.Value(key{})
	}))
	m.Publish(context.Background(), Record{RequestID: "blocker"})
	<-entered
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "billing-value"))
	cancel()
	record := Record{RequestID: "accounted", Generate: GenerateFlag(false), ResponseHeaders: http.Header{"X-Test": {"original"}}}
	if err := m.PublishContext(ctx, record); err != nil {
		t.Fatalf("completed request with capacity available: %v", err)
	}
	*record.Generate = true
	record.ResponseHeaders["X-Test"][0] = "mutated"
	unblock()
	shutdownTestManager(t, m)
	if gotErr != nil || gotValue != "billing-value" {
		t.Fatalf("plugin context err=%v value=%v", gotErr, gotValue)
	}
	if GenerateEnabled(got.Generate) || got.ResponseHeaders.Get("X-Test") != "original" {
		t.Fatalf("queued record changed: %#v", got)
	}
}

func TestManagerRestartIgnoresOldContextCancellation(t *testing.T) {
	m := NewManager(2)
	defer shutdownTestManager(t, m)
	oldCtx, cancelOld := context.WithCancel(context.Background())
	defer cancelOld()
	m.Start(oldCtx)
	shutdownTestManager(t, m)
	m.Start(context.Background())
	cancelOld()
	const total = 100
	count := 0
	m.Register(managerTestPlugin(func(context.Context, Record) { count++ }))
	for i := 0; i < total; i++ {
		if err := m.PublishContext(context.Background(), Record{}); err != nil {
			t.Fatalf("new generation PublishContext: %v", err)
		}
	}
	shutdownTestManager(t, m)
	if count != total {
		t.Fatalf("delivered = %d, want %d", count, total)
	}
}

func TestManagerConcurrentPublishAndLifecycle(t *testing.T) {
	m := NewManager(8)
	defer shutdownTestManager(t, m)
	var mu sync.Mutex
	accepted, delivered := make(map[string]int), make(map[string]int)
	m.Register(managerTestPlugin(func(_ context.Context, r Record) {
		mu.Lock()
		delivered[r.RequestID]++
		mu.Unlock()
	}))
	var wg sync.WaitGroup
	for worker := 0; worker < 32; worker++ {
		wg.Go(func() {
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("%d/%d", worker, i)
				err := m.PublishContext(context.Background(), Record{RequestID: id})
				if err == nil {
					mu.Lock()
					accepted[id]++
					mu.Unlock()
				} else if !errors.Is(err, ErrManagerStopped) {
					t.Errorf("PublishContext: %v", err)
				}
			}
		})
	}
	wg.Go(func() {
		for i := 0; i < 30; i++ {
			m.Stop()
			m.Start(context.Background())
		}
	})
	wg.Wait()
	shutdownTestManager(t, m)
	mu.Lock()
	defer mu.Unlock()
	if len(accepted) != len(delivered) {
		t.Fatalf("accepted %d records, delivered %d", len(accepted), len(delivered))
	}
	for id, n := range accepted {
		if n != 1 || delivered[id] != 1 {
			t.Fatalf("record %s accepted %d times, delivered %d times", id, n, delivered[id])
		}
	}
}

func TestManagerConcurrentPublishDrainsEveryAcceptedRecord(t *testing.T) {
	m := NewManager(16)
	defer shutdownTestManager(t, m)
	const workers, perWorker = 64, 100
	got := make(map[string]int, workers*perWorker)
	m.Register(managerTestPlugin(func(_ context.Context, r Record) { got[r.RequestID]++ }))
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Go(func() {
			for i := 0; i < perWorker; i++ {
				if err := m.PublishContext(context.Background(), Record{RequestID: fmt.Sprintf("%d/%d", worker, i)}); err != nil {
					t.Errorf("PublishContext: %v", err)
				}
			}
		})
	}
	wg.Wait()
	shutdownTestManager(t, m)
	if len(got) != workers*perWorker {
		t.Fatalf("delivered %d distinct records, want %d", len(got), workers*perWorker)
	}
	for id, count := range got {
		if count != 1 {
			t.Fatalf("record %s delivered %d times", id, count)
		}
	}
}

func TestManagerParentCancellationStopsIdleDispatcher(t *testing.T) {
	m := NewManager(1)
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	m.mu.Lock()
	run := m.running
	m.mu.Unlock()
	cancel()
	select {
	case <-run.done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled Start context did not stop idle dispatcher")
	}
	if err := m.PublishContext(context.Background(), Record{}); !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("PublishContext after parent cancellation = %v", err)
	}
}

func TestManagerPluginsReceiveIndependentRecordSnapshots(t *testing.T) {
	m := NewManager(1)
	defer shutdownTestManager(t, m)
	m.Register(managerTestPlugin(func(_ context.Context, r Record) {
		*r.Generate = true
		r.ResponseHeaders["X-Test"][0] = "mutated"
	}))
	var got Record
	m.Register(managerTestPlugin(func(_ context.Context, r Record) { got = r }))
	m.Publish(context.Background(), Record{Generate: GenerateFlag(false), ResponseHeaders: http.Header{"X-Test": {"original"}}})
	shutdownTestManager(t, m)
	if GenerateEnabled(got.Generate) || got.ResponseHeaders.Get("X-Test") != "original" {
		t.Fatalf("plugin mutation changed another plugin's record: %#v", got)
	}
}

func TestManagerStopBeforeStartDoesNotAutoRestart(t *testing.T) {
	m := NewManager(1)
	m.Stop()
	if err := m.PublishContext(context.Background(), Record{}); !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("PublishContext after Stop before Start = %v", err)
	}
	m.Start(context.Background())
	if err := m.PublishContext(context.Background(), Record{}); err != nil {
		t.Fatalf("PublishContext after explicit Start = %v", err)
	}
	shutdownTestManager(t, m)
}

func TestStreamFromContextHonorsExplicitTrue(t *testing.T) {
	ctx := WithStream(context.Background(), true)
	if !StreamFromContext(ctx) {
		t.Fatalf("StreamFromContext(true) = false, want true")
	}
}

func TestRecordStreamField(t *testing.T) {
	record := Record{
		Provider: "openai",
		Model:    "gpt-5.4",
		Stream:   true,
	}
	if !record.Stream {
		t.Fatalf("Record.Stream = false, want true")
	}
}

func TestGenerateEnabledDefaultsNilToTrue(t *testing.T) {
	if !GenerateEnabled(nil) {
		t.Fatalf("GenerateEnabled(nil) = false, want true")
	}
}

func TestGenerateEnabledHonorsExplicitFalse(t *testing.T) {
	if GenerateEnabled(GenerateFlag(false)) {
		t.Fatalf("GenerateEnabled(false) = true, want false")
	}
}

func TestGenerateEnabledHonorsExplicitTrue(t *testing.T) {
	if !GenerateEnabled(GenerateFlag(true)) {
		t.Fatalf("GenerateEnabled(true) = false, want true")
	}
}

func TestGenerateFromContextDefaultsMissingToTrue(t *testing.T) {
	if !GenerateFromContext(context.Background()) {
		t.Fatalf("GenerateFromContext(background) = false, want true")
	}
}

func TestGenerateFromContextHonorsExplicitFalse(t *testing.T) {
	ctx := WithGenerate(context.Background(), false)
	if GenerateFromContext(ctx) {
		t.Fatalf("GenerateFromContext(false) = true, want false")
	}
}

func TestRecordOmittedGenerateIsEnabled(t *testing.T) {
	// Existing callers construct Record without setting Generate.
	// Omission must remain distinguishable from explicit false and default to true.
	record := Record{
		Provider: "openai",
		Model:    "gpt-5.4",
	}
	if record.Generate != nil {
		t.Fatalf("Record.Generate = %v, want nil for omitted field", record.Generate)
	}
	if !GenerateEnabled(record.Generate) {
		t.Fatalf("GenerateEnabled(omitted) = false, want true")
	}
}
