package cache

// Shared thinking-replay cache infrastructure.
//
// The replay generation snapshot and the Home key-value client contract were
// originally declared in kimi_thinking_replay_cache.go, but the Claude
// compatible-API replay cache depends on both. They live here so the Claude path
// does not depend on any single provider's cache file surviving.
//
// Names are deliberately unchanged from their original declarations to keep the
// relocation a pure move with no call-site churn.

import (
	"context"
	"time"

	homekv "github.com/router-for-me/CLIProxyAPI/v7/internal/home"
)

const (
	// KimiThinkingReplayCacheTTL limits how long signed assistant content stays replayable.
	KimiThinkingReplayCacheTTL = 1 * time.Hour

	// KimiThinkingReplayCacheMaxEntries bounds process memory used for replay continuity.
	KimiThinkingReplayCacheMaxEntries = 10240

	// KimiThinkingReplayCacheEvictBatchSize leaves headroom after reaching capacity.
	KimiThinkingReplayCacheEvictBatchSize = 128

	// KimiThinkingReplayCacheMaxBytesPerEntry bounds one complete assistant content array.
	KimiThinkingReplayCacheMaxBytesPerEntry = 8 << 20

	// KimiThinkingReplayCacheMaxBlocksPerEntry prevents pathological content arrays.
	KimiThinkingReplayCacheMaxBlocksPerEntry = 512

	// KimiThinkingReplayCacheMaxTotalBytes bounds aggregate in-process replay content.
	KimiThinkingReplayCacheMaxTotalBytes = 256 << 20
)

// KimiThinkingReplaySnapshot identifies the exact replay generation read for one request.
type KimiThinkingReplaySnapshot struct {
	raw        []byte
	generation string
	loaded     bool
	found      bool
}

// kimiThinkingReplayKVClient is the Home key-value surface used by the bounded
// thinking-replay caches.
type kimiThinkingReplayKVClient interface {
	KVGet(ctx context.Context, key string) ([]byte, bool, error)
	KVSet(ctx context.Context, key string, value []byte, opts homekv.KVSetOptions) (bool, error)
	KVDel(ctx context.Context, keys ...string) (int64, error)
	KVCompareAndSwap(ctx context.Context, key string, expected []byte, expectedExists bool, value []byte, ttl time.Duration) (bool, error)
	KVExpire(ctx context.Context, key string, ttl time.Duration) (bool, error)
}
