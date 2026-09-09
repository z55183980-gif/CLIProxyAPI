package usage

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// DefaultServiceTier is retained for direct SDK and non-OpenAI usage callers.
const DefaultServiceTier = "default"

// AutoServiceTier is the OpenAI request semantics when service_tier is omitted.
// OpenAI HTTP handlers set it explicitly, without changing other providers'
// historical direct-SDK default.
const AutoServiceTier = "auto"

// Record contains the usage statistics captured for a single provider request.
type Record struct {
	// RequestID is the stable client/proxy request identifier when available.
	// Billing sinks should use it as the primary idempotency key.
	RequestID string
	Provider  string
	// ExecutorType stores the concrete executor type that handled the request.
	ExecutorType string
	Model        string
	Alias        string
	APIKey       string
	AuthID       string
	AuthIndex    string
	// AccessTokenSHA256 identifies the OAuth token version without exposing the token.
	AccessTokenSHA256 string
	AuthType          string
	Source            string
	// ReasoningEffort stores the translated upstream thinking level for request event logs.
	ReasoningEffort string
	// ServiceTier stores the client-requested service tier.
	ServiceTier string
	// RequestServiceTier is a deprecated input-only alias retained for existing
	// plugin callers. It is normalized into ServiceTier and never emitted.
	RequestServiceTier string
	// ResponseServiceTier stores the final tier reported by the upstream response.
	ResponseServiceTier string
	// Generate reports whether the client requested actual generation.
	// nil or true means generation is enabled; only an explicit false disables generation.
	// Use GenerateFlag to set the value and GenerateEnabled to read it with the default.
	Generate *bool
	// Stream reports whether the request was executed in streaming mode.
	Stream      bool
	RequestedAt time.Time
	Latency     time.Duration
	TTFT        time.Duration
	Failed      bool
	Fail        Failure
	Detail      Detail
	// ResponseHeaders stores a snapshot of upstream response headers for usage sinks.
	ResponseHeaders http.Header
}

// Failure holds HTTP failure metadata for an upstream request attempt.
type Failure struct {
	StatusCode int
	Body       string
}

// Detail holds the token usage breakdown.
type Detail struct {
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
	TokenBreakdown      TokenBreakdown
	ResponseServiceTier string
}

type requestedModelAliasContextKey struct{}
type reasoningEffortContextKey struct{}
type serviceTierContextKey struct{}
type generateContextKey struct{}
type streamContextKey struct{}

// WithRequestedModelAlias stores the client-requested model name for usage sinks.
func WithRequestedModelAlias(ctx context.Context, alias string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return ctx
	}
	return context.WithValue(ctx, requestedModelAliasContextKey{}, alias)
}

// RequestedModelAliasFromContext returns the client-requested model name stored in ctx.
func RequestedModelAliasFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(requestedModelAliasContextKey{})
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

// WithReasoningEffort stores the client-requested reasoning effort for usage sinks.
func WithReasoningEffort(ctx context.Context, effort string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return ctx
	}
	return context.WithValue(ctx, reasoningEffortContextKey{}, effort)
}

// ReasoningEffortFromContext returns the client-requested reasoning effort stored in ctx.
func ReasoningEffortFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	raw := ctx.Value(reasoningEffortContextKey{})
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		return ""
	}
}

// WithServiceTier stores the client-requested service tier for usage sinks.
func WithServiceTier(ctx context.Context, tier string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	tier = strings.TrimSpace(tier)
	if tier == "" {
		tier = DefaultServiceTier
	}
	return context.WithValue(ctx, serviceTierContextKey{}, tier)
}

// ServiceTierFromContext returns the client-requested service tier stored in ctx.
func ServiceTierFromContext(ctx context.Context) string {
	if ctx == nil {
		return DefaultServiceTier
	}
	raw := ctx.Value(serviceTierContextKey{})
	switch value := raw.(type) {
	case string:
		tier := strings.TrimSpace(value)
		if tier == "" {
			return DefaultServiceTier
		}
		return tier
	case []byte:
		tier := strings.TrimSpace(string(value))
		if tier == "" {
			return DefaultServiceTier
		}
		return tier
	default:
		return DefaultServiceTier
	}
}

// WithGenerate stores whether the client requested actual generation for usage sinks.
// Missing context values default to true; only an explicit false disables generation.
func WithGenerate(ctx context.Context, generate bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, generateContextKey{}, generate)
}

// GenerateFromContext returns whether the client requested actual generation.
// Missing values default to true.
func GenerateFromContext(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	raw := ctx.Value(generateContextKey{})
	switch value := raw.(type) {
	case bool:
		return value
	default:
		return true
	}
}

// WithStream stores whether the request was executed in streaming mode for usage sinks.
func WithStream(ctx context.Context, stream bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, streamContextKey{}, stream)
}

// StreamFromContext returns whether the request was executed in streaming mode.
// Missing values default to false.
func StreamFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	raw := ctx.Value(streamContextKey{})
	switch value := raw.(type) {
	case bool:
		return value
	default:
		return false
	}
}

// GenerateFlag returns a pointer suitable for Record.Generate.
func GenerateFlag(generate bool) *bool {
	return &generate
}

// GenerateEnabled reports whether generation is enabled for the record field.
// A nil value defaults to true so legacy callers that omit Generate keep the historical behavior.
func GenerateEnabled(generate *bool) bool {
	if generate == nil {
		return true
	}
	return *generate
}

// Plugin consumes usage records emitted by the proxy runtime.
type Plugin interface {
	HandleUsage(ctx context.Context, record Record)
}

type queueItem struct {
	ctx    context.Context
	record Record
}

// ErrManagerStopped indicates that a record was not accepted because the
// manager has stopped or is draining accepted records.
var ErrManagerStopped = errors.New("usage manager is stopped")

// managerRun owns one dispatcher generation. Cancellation of an old Start
// context must never close a newly started dispatcher.
type managerRun struct {
	queue   []queueItem
	head    int
	size    int
	closing bool
	changed chan struct{}
	done    chan struct{}
}

// Manager maintains a queue of usage records and delivers them to registered plugins.
type Manager struct {
	mu          sync.Mutex
	running     *managerRun
	initialized bool
	capacity    int

	pluginsMu sync.RWMutex
	plugins   []Plugin
	named     map[string]int
}

// NewManager constructs a manager with a buffered queue.
func NewManager(buffer int) *Manager {
	if buffer <= 0 {
		buffer = 512
	}
	return &Manager{capacity: buffer}
}

// Start launches the background dispatcher. Calling Start multiple times is safe.
// A stopped manager may be restarted after Shutdown has finished draining it.
// Start is a no-op while the current dispatcher is still running or draining.
func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running == nil {
		m.startLocked(ctx)
	}
}

func (m *Manager) startLocked(ctx context.Context) {
	if m.capacity <= 0 {
		m.capacity = 512
	}
	run := &managerRun{
		queue:   make([]queueItem, m.capacity),
		changed: make(chan struct{}),
		done:    make(chan struct{}),
	}
	m.running = run
	m.initialized = true
	go m.run(run)
	if ctx != nil && ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				m.mu.Lock()
				if m.running == run {
					m.stopLocked()
				}
				m.mu.Unlock()
			case <-run.done:
			}
		}()
	}
}

// Stop rejects new records and starts draining accepted records. It does not
// wait for plugins to finish; use Shutdown before closing plugin resources.
func (m *Manager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.stopLocked()
	m.mu.Unlock()
}

func (m *Manager) stopLocked() *managerRun {
	m.initialized = true
	run := m.running
	if run != nil && !run.closing {
		run.closing = true
		notifyRunLocked(run)
	}
	return run
}

// Shutdown rejects new records and waits until all accepted records have been
// delivered. The context limits waiting only: cancellation does not discard
// queued records or cancel plugin work. Call Shutdown again to await completion.
func (m *Manager) Shutdown(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	run := m.stopLocked()
	m.mu.Unlock()
	if run == nil {
		return nil
	}
	select {
	case <-run.done:
		return nil
	default:
	}
	select {
	case <-run.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Register appends a plugin to the delivery list.
func (m *Manager) Register(plugin Plugin) {
	if m == nil || plugin == nil {
		return
	}
	m.pluginsMu.Lock()
	m.plugins = append(m.plugins, plugin)
	m.pluginsMu.Unlock()
}

// RegisterNamed registers or replaces a plugin by name.
func (m *Manager) RegisterNamed(name string, plugin Plugin) {
	if m == nil || plugin == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	m.pluginsMu.Lock()
	if m.named == nil {
		m.named = make(map[string]int)
	}
	if index, exists := m.named[name]; exists && index >= 0 && index < len(m.plugins) {
		m.plugins[index] = plugin
		m.pluginsMu.Unlock()
		return
	}
	m.named[name] = len(m.plugins)
	m.plugins = append(m.plugins, plugin)
	m.pluginsMu.Unlock()
}

// Publish enqueues a usage record for processing. If no plugin is registered
// the record will be discarded downstream.
func (m *Manager) Publish(ctx context.Context, record Record) {
	if err := m.PublishContext(ctx, record); err != nil {
		log.Warnf("usage: record was not queued: %v", err)
	}
}

// PublishContext enqueues a record, waiting for capacity when the bounded queue
// is full. Cancellation only aborts that wait; a record with an available slot
// is accepted even if the request has already ended. Accepted records retain
// context values but are delivered independently of request cancellation.
func (m *Manager) PublishContext(ctx context.Context, record Record) error {
	if m == nil {
		return ErrManagerStopped
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if !m.initialized {
		m.startLocked(context.Background())
	}
	run := m.running
	for {
		if run == nil || run.closing || m.running != run {
			m.mu.Unlock()
			return ErrManagerStopped
		}
		if run.size < len(run.queue) {
			tail := (run.head + run.size) % len(run.queue)
			run.queue[tail] = queueItem{ctx: context.WithoutCancel(ctx), record: cloneQueuedRecord(record)}
			run.size++
			notifyRunLocked(run)
			m.mu.Unlock()
			return nil
		}
		changed := run.changed
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
		m.mu.Lock()
	}
}

func cloneQueuedRecord(record Record) Record {
	if record.Generate != nil {
		record.Generate = GenerateFlag(*record.Generate)
	}
	record.ResponseHeaders = record.ResponseHeaders.Clone()
	return record
}

func notifyRunLocked(run *managerRun) {
	close(run.changed)
	run.changed = make(chan struct{})
}

func (m *Manager) run(run *managerRun) {
	for {
		m.mu.Lock()
		if run.size == 0 {
			if run.closing {
				if m.running == run {
					m.running = nil
				}
				close(run.done)
				m.mu.Unlock()
				return
			}
			changed := run.changed
			m.mu.Unlock()
			<-changed
			continue
		}
		item := run.queue[run.head]
		run.queue[run.head] = queueItem{}
		run.head = (run.head + 1) % len(run.queue)
		run.size--
		notifyRunLocked(run)
		m.mu.Unlock()
		m.dispatch(item)
	}
}

func (m *Manager) dispatch(item queueItem) {
	m.pluginsMu.RLock()
	plugins := make([]Plugin, len(m.plugins))
	copy(plugins, m.plugins)
	m.pluginsMu.RUnlock()
	if len(plugins) == 0 {
		return
	}
	for _, plugin := range plugins {
		if plugin == nil {
			continue
		}
		safeInvoke(plugin, item.ctx, cloneQueuedRecord(item.record))
	}
}

func safeInvoke(plugin Plugin, ctx context.Context, record Record) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("usage: plugin panic recovered: %v", r)
		}
	}()
	plugin.HandleUsage(ctx, record)
}

var defaultManager = NewManager(512)

// DefaultManager returns the global usage manager instance.
func DefaultManager() *Manager { return defaultManager }

// RegisterPlugin registers a plugin on the default manager.
func RegisterPlugin(plugin Plugin) { DefaultManager().Register(plugin) }

// RegisterNamedPlugin registers or replaces a named plugin on the default manager.
func RegisterNamedPlugin(name string, plugin Plugin) { DefaultManager().RegisterNamed(name, plugin) }

// PublishRecord publishes a record using the default manager.
func PublishRecord(ctx context.Context, record Record) { DefaultManager().Publish(ctx, record) }

// StartDefault starts the default manager's dispatcher.
func StartDefault(ctx context.Context) { DefaultManager().Start(ctx) }

// StopDefault stops the default manager's dispatcher.
func StopDefault() { DefaultManager().Stop() }

// ShutdownDefault waits for accepted records before plugin resources are closed.
func ShutdownDefault(ctx context.Context) error { return DefaultManager().Shutdown(ctx) }
