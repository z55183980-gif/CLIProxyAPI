package cliproxy

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

type startupTokenProviderFunc func(context.Context, *config.Config) (*TokenClientResult, error)

func (f startupTokenProviderFunc) Load(ctx context.Context, cfg *config.Config) (*TokenClientResult, error) {
	return f(ctx, cfg)
}

func TestServiceShutdownDuringStartupDoesNotResumeInitialization(t *testing.T) {
	for _, phase := range []string{"token-provider", "before-start-hook"} {
		t.Run(phase, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var afterStart, watcherCreated atomic.Bool
			cfg := &config.Config{AuthDir: t.TempDir(), Host: "127.0.0.1", Port: 0}
			service := &Service{
				cfg:         cfg,
				configPath:  filepath.Join(t.TempDir(), "config.yaml"),
				coreManager: coreauth.NewManager(nil, nil, nil),
				tokenProvider: startupTokenProviderFunc(func(ctx context.Context, _ *config.Config) (*TokenClientResult, error) {
					if phase == "token-provider" {
						close(entered)
						// Deliberately ignore cancellation until released, as third-party
						// SDK loaders and startup hooks may do.
						<-release
						return nil, ctx.Err()
					}
					return &TokenClientResult{}, nil
				}),
				apiKeyProvider: NewAPIKeyClientProvider(),
				watcherFactory: func(string, string, func(*config.Config)) (*WatcherWrapper, error) {
					watcherCreated.Store(true)
					return &WatcherWrapper{}, nil
				},
				hooks: Hooks{
					OnBeforeStart: func(*config.Config) {
						if phase == "before-start-hook" {
							close(entered)
							<-release
						}
					},
					OnAfterStart: func(*Service) { afterStart.Store(true) },
				},
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer unblock()
			done := make(chan error, 1)
			go func() { done <- service.Run(ctx) }()
			select {
			case <-entered:
			case err := <-done:
				t.Fatalf("Run stopped before startup gate: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("Run did not enter startup gate")
			}
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancelShutdown()
			shutdownDone := make(chan error, 1)
			go func() { shutdownDone <- service.Shutdown(shutdownCtx) }()
			select {
			case err := <-shutdownDone:
				if phase == "token-provider" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Shutdown while startup blocked = %v", err)
				}
				if phase == "before-start-hook" && err != nil {
					t.Fatalf("Shutdown while hook blocked = %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Shutdown ignored deadline while startup blocked")
			}
			replacement := &Service{}
			if phase == "token-provider" {
				if err := replacement.claimUsageLifecycle(); err == nil {
					replacement.releaseUsageLifecycle()
					t.Fatal("usage lifecycle released while old initialization could still mutate global hooks")
				}
			}
			unblock()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Run after startup cancellation = %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Run did not finish deferred shutdown")
			}
			if service.serverErr != nil || watcherCreated.Load() || afterStart.Load() {
				t.Fatal("service resumed initialization after shutdown requested")
			}
			if err := replacement.claimUsageLifecycle(); err != nil {
				t.Fatalf("owner retained after deferred cleanup: %v", err)
			}
			replacement.releaseUsageLifecycle()
		})
	}
}

func TestServiceAfterStartHookCanShutdownSynchronously(t *testing.T) {
	var watcherCreated atomic.Bool
	shutdownResult := make(chan error, 1)
	service := &Service{
		cfg:         &config.Config{AuthDir: t.TempDir(), Host: "127.0.0.1", Port: 0},
		configPath:  filepath.Join(t.TempDir(), "config.yaml"),
		coreManager: coreauth.NewManager(nil, nil, nil),
		tokenProvider: startupTokenProviderFunc(func(context.Context, *config.Config) (*TokenClientResult, error) {
			return &TokenClientResult{}, nil
		}),
		apiKeyProvider: NewAPIKeyClientProvider(),
		watcherFactory: func(string, string, func(*config.Config)) (*WatcherWrapper, error) {
			watcherCreated.Store(true)
			return &WatcherWrapper{}, nil
		},
		hooks: Hooks{OnAfterStart: func(running *Service) {
			// This synchronous SDK callback must not wait on the startup lock
			// already held by its own Run call.
			shutdownResult <- running.Shutdown(context.Background())
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	select {
	case err := <-shutdownResult:
		if err != nil {
			t.Fatalf("hook Shutdown = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnAfterStart deadlocked calling Shutdown")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run after hook Shutdown = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not finish after hook Shutdown")
	}
	if watcherCreated.Load() {
		t.Fatal("Run created file watcher after hook shut service down")
	}
	replacement := &Service{}
	if err := replacement.claimUsageLifecycle(); err != nil {
		t.Fatalf("hook Shutdown retained runtime owner: %v", err)
	}
	replacement.releaseUsageLifecycle()
}
