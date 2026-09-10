package usage

import (
	"context"
	"sync/atomic"
	"testing"
)

type drainPlugin struct{ count atomic.Int64 }

func (p *drainPlugin) HandleUsage(context.Context, Record) { p.count.Add(1) }
func TestStoppedDispatcherDrainsBeforeStorageClose(t *testing.T) {
	m := NewManager(1)
	p := &drainPlugin{}
	m.Register(p)
	for i := 0; i < 100; i++ {
		m.Publish(context.Background(), Record{})
	}
	m.Stop()
	m.WaitStopped()
	if p.count.Load() != 100 {
		t.Fatalf("lost queued usage records: %d", p.count.Load())
	}
}
