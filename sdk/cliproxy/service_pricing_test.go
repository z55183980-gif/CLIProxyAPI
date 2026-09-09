package cliproxy

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

func TestPricingOwnerPreventsCrossServicePluginReplacement(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.Enabled = true
	cfg.Pricing.InMemory = true
	cfg.Pricing.RateMultiplier = 1
	cfg.Pricing.Revision = "test"
	cfg.Pricing.Rules = []usage.PriceRule{{Model: "claude-test*", PriceCard: usage.PriceCard{InputPerToken: 1}}}
	first, second := &Service{}, &Service{}
	if err := first.configurePricing(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.closePricing(); second.closePricing() })
	if err := second.configurePricing(context.Background(), cfg); err == nil {
		t.Fatal("second service unexpectedly acquired the global pricing plugin")
	}
	first.closePricing()
	if err := second.configurePricing(context.Background(), cfg); err != nil {
		t.Fatalf("second service could not acquire released pricing plugin: %v", err)
	}
}
