package cache

import (
	"bytes"
	"context"
	"sync"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
)

// Shared in-memory Home key-value fake for the thinking-replay cache tests.
//
// This was originally declared in kimi_thinking_replay_cache_test.go, which was
// removed with the Kimi upstream. The Claude replay cache tests depend on it, so
// it lives here now. The name is unchanged to keep the relocation a pure move.

type fakeKimiThinkingReplayKVClient struct {
	mu     sync.Mutex
	values map[string][]byte
}

func newFakeKimiThinkingReplayKVClient() *fakeKimiThinkingReplayKVClient {
	return &fakeKimiThinkingReplayKVClient{values: make(map[string][]byte)}
}

func (c *fakeKimiThinkingReplayKVClient) KVGet(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, found := c.values[key]
	return append([]byte(nil), value...), found, nil
}

func (c *fakeKimiThinkingReplayKVClient) KVSet(_ context.Context, key string, value []byte, _ homekv.KVSetOptions) (bool, error) {
	c.mu.Lock()
	c.values[key] = append([]byte(nil), value...)
	c.mu.Unlock()
	return true, nil
}

func (c *fakeKimiThinkingReplayKVClient) KVDel(_ context.Context, keys ...string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var deleted int64
	for _, key := range keys {
		if _, found := c.values[key]; found {
			delete(c.values, key)
			deleted++
		}
	}
	return deleted, nil
}

func (c *fakeKimiThinkingReplayKVClient) KVCompareAndSwap(_ context.Context, key string, expected []byte, expectedExists bool, value []byte, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	current, found := c.values[key]
	if found != expectedExists || (found && !bytes.Equal(current, expected)) {
		return false, nil
	}
	c.values[key] = append([]byte(nil), value...)
	return true, nil
}

func (c *fakeKimiThinkingReplayKVClient) KVExpire(_ context.Context, key string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, found := c.values[key]
	return found, nil
}
