package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

const capacityExceededCode = "credential_capacity_exceeded"

// ParseCapacity validates a credential's local concurrency limit. Zero means unlimited.
func ParseCapacity(value any) (int, error) {
	if value == nil {
		return 0, nil
	}
	raw := fmt.Sprint(value)
	if number, ok := value.(json.Number); ok {
		raw = number.String()
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 || limit > 1000000 {
		return 0, fmt.Errorf("concurrency must be an integer between 0 and 1000000")
	}
	return limit, nil
}

// AuthCapacity returns the configured local concurrency limit.
func AuthCapacity(auth *Auth) int {
	if auth == nil {
		return 0
	}
	limit, _ := ParseCapacity(auth.Metadata["concurrency"])
	return limit
}

// CurrentConcurrency returns the number of active upstream attempts for a credential.
func (m *Manager) CurrentConcurrency(id string) int {
	if m == nil {
		return 0
	}
	m.capacityMu.Lock()
	defer m.capacityMu.Unlock()
	return m.activeRequests[id]
}

func (m *Manager) acquireCapacity(auth *Auth) (func(), error) {
	m.mu.RLock()
	current := m.auths[auth.ID]
	if current == nil {
		current = auth
	}
	limit := AuthCapacity(current)
	m.capacityMu.Lock()
	defer m.capacityMu.Unlock()
	defer m.mu.RUnlock()
	// Home owns distributed admission; local limits apply only to local dispatch.
	if !m.HomeEnabled() && limit > 0 && m.activeRequests[auth.ID] >= limit {
		return nil, &Error{Code: capacityExceededCode, Message: "credential concurrency limit reached", Retryable: true, HTTPStatus: http.StatusTooManyRequests}
	}
	if m.activeRequests == nil {
		m.activeRequests = make(map[string]int)
	}
	id := auth.ID
	m.activeRequests[id]++
	var once sync.Once
	return func() {
		once.Do(func() {
			m.capacityMu.Lock()
			defer m.capacityMu.Unlock()
			m.activeRequests[id]--
			if m.activeRequests[id] == 0 {
				delete(m.activeRequests, id)
			}
		})
	}, nil
}

func (m *Manager) executeWithCapacity(ctx context.Context, executor ProviderExecutor, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, countTokens bool) (cliproxyexecutor.Response, error) {
	if countTokens {
		release, err := m.acquireCapacity(auth)
		if err != nil {
			return cliproxyexecutor.Response{}, err
		}
		defer release()
		return executor.CountTokens(ctx, auth, req, opts)
	}
	if flow := sub2apiState(ctx); flow != nil {
		flow.model = req.Model
		flow.headers = nil
	}
	var response cliproxyexecutor.Response
	err := m.sub2apiAttempt(ctx, auth, func() error {
		release, errAcquire := m.acquireCapacity(auth)
		if errAcquire != nil {
			return errAcquire
		}
		defer release()
		var errExec error
		response, errExec = executor.Execute(ctx, auth, req, opts)
		return errExec
	}, false)
	return response, err
}

func (m *Manager) streamWithCapacity(ctx context.Context, executor ProviderExecutor, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	if sub2apiState(ctx) != nil {
		return m.sub2apiStream(ctx, executor, auth, req, opts)
	}
	return m.streamOnceWithCapacity(ctx, executor, auth, req, opts)
}

func (m *Manager) streamOnceWithCapacity(ctx context.Context, executor ProviderExecutor, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	release, err := m.acquireCapacity(auth)
	if err != nil {
		return nil, err
	}
	result, errStream := executor.ExecuteStream(ctx, auth, req, opts)
	if errStream != nil || result == nil || result.Chunks == nil {
		release()
		return result, errStream
	}
	out := make(chan cliproxyexecutor.StreamChunk)
	stopRelease := context.AfterFunc(ctx, release)
	go func() {
		defer close(out)
		defer release()
		defer stopRelease()
		// Preserve the original stream's cancellation/error chunks and draining behavior.
		for chunk := range result.Chunks {
			out <- chunk
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: result.Headers, Chunks: out}, nil
}
