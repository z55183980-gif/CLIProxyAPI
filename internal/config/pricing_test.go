package config

import (
	"math"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestPricingConfigRejectsNonFiniteMultipliers(t *testing.T) {
	for _, rate := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, enabled := range []bool{false, true} {
			cfg := PricingConfig{Enabled: enabled, RateMultiplier: rate, PricingTable: usage.PricingTable{Revision: "test", Default: &usage.PriceCard{InputPerToken: 1}}}
			if err := cfg.Validate(); err == nil {
				t.Fatalf("accepted rate %v when enabled=%v", rate, enabled)
			}
		}
	}
}

func TestPricingConfigRejectsNonFiniteYAMLMultipliers(t *testing.T) {
	for _, rate := range []string{".nan", ".inf", "-.inf"} {
		t.Run(rate, func(t *testing.T) {
			if _, err := ParseConfigBytes([]byte("pricing:\n  rate-multiplier: " + rate + "\n")); err == nil {
				t.Fatalf("accepted YAML rate-multiplier %s", rate)
			}
		})
	}
}

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
