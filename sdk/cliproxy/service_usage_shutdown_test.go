package cliproxy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

type gatedBillingSink struct {
	sink    *usage.MemoryChargeSink
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *gatedBillingSink) Apply(ctx context.Context, charge usage.Charge) error {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.sink.Apply(ctx, charge)
}

func TestServiceShutdownDrainsBillingBeforeClosing(t *testing.T) {
	for _, cancelWait := range []bool{false, true} {
		name := "graceful"
		if cancelWait {
			name = "canceled_wait"
		}
		t.Run(name, func(t *testing.T) {
			if err := usage.ShutdownDefault(context.Background()); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{}
			cfg.Pricing.Enabled = true
			cfg.Pricing.InMemory = true
			cfg.Pricing.RateMultiplier = 1
			cfg.Pricing.Revision = "shutdown-test"
			cfg.Pricing.Rules = []usage.PriceRule{{Model: "claude-test", PriceCard: usage.PriceCard{InputPerToken: 1}}}
			service := &Service{}
			if err := service.claimUsageLifecycle(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(service.releaseUsageLifecycle)
			if err := service.configurePricing(context.Background(), cfg); err != nil {
				t.Fatal(err)
			}
			sink := &gatedBillingSink{sink: usage.NewMemoryChargeSink(), entered: make(chan struct{}), release: make(chan struct{})}
			usage.RegisterNamedPlugin(pricingPluginName, &usage.PricingPlugin{
				Engine: usage.NewPriceEngine(cfg.Pricing.PricingTable), Sink: sink,
				Totals: service.pricingTotals, RateMultiplier: 1,
			})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(sink.release) }) }
			t.Cleanup(func() {
				release()
				_ = usage.ShutdownDefault(context.Background())
				service.closePricing()
			})
			usage.StartDefault(context.Background())
			for _, id := range []string{"first", "last"} {
				usage.PublishRecord(context.Background(), usage.Record{RequestID: id, Provider: "claude", Model: "claude-test", Detail: usage.Detail{InputTokens: 10, TotalTokens: 10}})
			}
			select {
			case <-sink.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("billing did not start")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelWait {
				cancel()
			}
			done := make(chan error, 1)
			go func() { done <- service.Shutdown(ctx) }()
			if cancelWait {
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("Shutdown error = %v, want cancellation", err)
					}
					if err := service.Shutdown(context.Background()); !errors.Is(err, context.Canceled) {
						t.Fatalf("repeated Shutdown lost its error: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("Shutdown ignored cancellation")
				}
			} else {
				select {
				case err := <-done:
					t.Fatalf("Shutdown returned before billing finished: %v", err)
				case <-time.After(20 * time.Millisecond):
				}
			}
			if service.PricingTotals() == nil {
				t.Fatal("billing closed before queued records drained")
			}
			release()
			if !cancelWait {
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("Shutdown did not finish")
				}
			}
			deadline := time.Now().Add(5 * time.Second)
			for service.PricingTotals() != nil && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if service.PricingTotals() != nil {
				t.Fatal("billing resources were not released after drain")
			}
			if got := len(sink.sink.Charges()); got != 2 {
				t.Fatalf("stored %d charges, want both queued requests", got)
			}
		})
	}
}

func TestInactiveServiceShutdownDoesNotStopActiveUsage(t *testing.T) {
	_ = usage.ShutdownDefault(context.Background())
	active, inactive := &Service{}, &Service{}
	if err := active.claimUsageLifecycle(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = active.Shutdown(context.Background()) })
	usage.StartDefault(context.Background())
	if err := inactive.claimUsageLifecycle(); err == nil {
		t.Fatal("second service acquired the process usage runtime")
	}
	if err := inactive.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := usage.DefaultManager().PublishContext(context.Background(), usage.Record{}); err != nil {
		t.Fatalf("inactive shutdown stopped active usage: %v", err)
	}
	if err := active.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := inactive.claimUsageLifecycle(); err == nil {
		t.Fatal("shut-down service was allowed to start")
	}
	if err := active.claimUsageLifecycle(); err == nil {
		t.Fatal("active service was allowed to restart after shutdown")
	}
	replacement := &Service{}
	if err := replacement.claimUsageLifecycle(); err != nil {
		t.Fatal(err)
	}
	replacement.releaseUsageLifecycle()
}

func TestPricingReloadKeepsInMemoryCharges(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.Enabled, cfg.Pricing.InMemory = true, true
	cfg.Pricing.RateMultiplier = 1
	cfg.Pricing.Revision = "before"
	cfg.Pricing.Rules = []usage.PriceRule{{Model: "claude-test", PriceCard: usage.PriceCard{InputPerToken: 1}}}
	service := &Service{}
	if err := service.configurePricing(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.closePricing)
	sink := service.pricingSink.(*usage.MemoryChargeSink)
	totals := service.pricingTotals
	charge := usage.Charge{EventID: "before-reload", Quote: usage.TokenQuote{TotalMicros: 100}}
	if err := sink.Apply(context.Background(), charge); err != nil {
		t.Fatal(err)
	}
	totals.Add(charge)
	cfg.Pricing.Revision = "after"
	if err := service.configurePricing(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if service.pricingSink != sink || service.pricingTotals != totals || len(sink.Charges()) != 1 {
		t.Fatal("reload discarded accumulated in-memory billing data")
	}
}
