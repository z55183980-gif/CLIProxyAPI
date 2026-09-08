package config

import "testing"

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
