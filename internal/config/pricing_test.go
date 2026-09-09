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

func TestPricingConfigValidationRequiresTableWhenEnabled(t *testing.T) {
	cfg := Config{}
	cfg.Pricing.Enabled = true
	cfg.Pricing.RateMultiplier = 1
	cfg.Pricing.Revision = "test"
	cfg.Pricing.Default = nil
	if err := cfg.Pricing.Validate(); err == nil {
		t.Fatal("expected enabled pricing without a pricing table to fail validation")
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

func TestPricingConfigParsesInMemory(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte("pricing:\n  enabled: true\n  in-memory: true\n  rate-multiplier: 1\n  revision: test\n  default:\n    input-per-token: 0\n"))
	if err != nil {
		t.Fatalf("ParseConfigBytes: %v", err)
	}
	if !cfg.Pricing.InMemory {
		t.Fatal("pricing in-memory flag was not parsed")
	}
	if err := cfg.Pricing.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
