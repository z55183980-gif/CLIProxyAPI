package config

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestPricingConfigValidationDisabledByDefault(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte("port: 8317\n"))
	if err != nil {
		t.Fatalf("ParseConfigBytes: %v", err)
	}
	if cfg.Pricing.Enabled {
		t.Fatal("pricing must be disabled by default")
	}
}

func TestPricingConfigValidationRequiresDSNWhenEnabled(t *testing.T) {
	cfg := Config{}
	cfg.Pricing.Enabled = true
	cfg.Pricing.Default = nil
	if err := cfg.Pricing.Validate(); err == nil {
		t.Fatal("expected enabled pricing without table/DSN to fail validation")
	}
}

func TestPricingConfigValidationAllowsInMemoryWithoutDSN(t *testing.T) {
	cfg := Config{}
	cfg.Pricing.Enabled = true
	cfg.Pricing.InMemory = true
	cfg.Pricing.RateMultiplier = 1
	cfg.Pricing.Revision = "test"
	cfg.Pricing.Default = &usage.PriceCard{InputPerToken: 0.000001, OutputPerToken: 0.000002}
	if err := cfg.Pricing.Validate(); err != nil {
		t.Fatalf("in-memory pricing should validate without DSN: %v", err)
	}
}
