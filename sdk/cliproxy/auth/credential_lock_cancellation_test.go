package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCredentialMutationLockRejectsAlreadyCanceledContext(t *testing.T) {
	// Both the idle lock and ctx.Done are ready. Selecting randomly between
	// them previously let canceled requests start profile/refresh HTTP calls.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lock := &requestAuthPrepareLock{}
	for i := 0; i < 128; i++ {
		if err := lock.acquire(ctx); !errors.Is(err, context.Canceled) {
			if err == nil {
				lock.release()
			}
			t.Fatalf("acquire canceled context = %v", err)
		}
	}
	if err := lock.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	lock.release()
}

func TestClaudeCredentialMutationWaitHonorsCancellation(t *testing.T) {
	for _, path := range []string{"prepare", "home prepare", "refresh"} {
		t.Run(path, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			executor := &claudeCancellationTestExecutor{}
			block := func(_ context.Context, auth *Auth) (*Auth, error) {
				select {
				case <-entered:
				default:
					close(entered)
				}
				<-release
				return auth, nil
			}
			executor.prepareFn = block
			executor.refreshFn = block
			manager, auth, _ := newClaudeCancellationTestManager(t, executor, nil)
			run := func(ctx context.Context) error {
				var err error
				switch path {
				case "prepare":
					_, err = manager.prepareRequestAuth(ctx, executor, auth)
				case "home prepare":
					_, err = manager.prepareHomeAuthSnapshot(ctx, executor, auth)
				case "refresh":
					_, err = manager.refreshAuthForRequest(ctx, auth.ID, "")
				}
				return err
			}
			first := make(chan error, 1)
			go func() { first <- run(context.Background()) }()
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("first request never entered credential work")
			}
			ctx, cancel := context.WithCancel(context.Background())
			second := make(chan error, 1)
			go func() { second <- run(ctx) }()
			cancel()
			select {
			case err := <-second:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("waiting request = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("canceled request stayed blocked behind credential work")
			}
			if got := executor.prepareCalls.Load() + executor.refreshCalls.Load(); got != 1 {
				t.Fatalf("credential work calls = %d, want only the first request", got)
			}
			unblock()
			select {
			case err := <-first:
				if err != nil {
					t.Fatalf("first request failed: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("first request did not release the lock")
			}
			recovery := make(chan error, 1)
			go func() { recovery <- run(context.Background()) }()
			select {
			case err := <-recovery:
				if err != nil {
					t.Fatalf("request after cancellation failed: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("canceled waiter leaked the credential lock")
			}
		})
	}
}
