package config

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

// PricingConfig controls optional token pricing and durable billing.
// Pricing is disabled by default and is independent from usage statistics.
type PricingConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	// DatabaseDSNEnv names an environment variable containing the PostgreSQL DSN.
	// Keeping the DSN out of YAML prevents accidental exposure via management APIs.
	DatabaseDSNEnv     string  `yaml:"database-dsn-env,omitempty" json:"database-dsn-env,omitempty"`
	DatabaseTable      string  `yaml:"database-table,omitempty" json:"database-table,omitempty"`
	RateMultiplier     float64 `yaml:"rate-multiplier,omitempty" json:"rate-multiplier,omitempty"`
	usage.PricingTable `yaml:",inline" json:",inline"`
}

func (p PricingConfig) Validate() error {
	if p.RateMultiplier < 0 {
		return fmt.Errorf("pricing rate-multiplier must be non-negative")
	}
	if !p.Enabled {
		return nil
	}
	if p.RateMultiplier == 0 {
		return fmt.Errorf("pricing rate-multiplier must be greater than zero when enabled")
	}
	if err := p.PricingTable.Validate(); err != nil {
		return fmt.Errorf("pricing table: %w", err)
	}
	return nil
}

func (p PricingConfig) DSNEnv() string {
	if value := strings.TrimSpace(p.DatabaseDSNEnv); value != "" {
		return value
	}
	return "CPA_PRICING_DATABASE_DSN"
}

func (p PricingConfig) TableName() string {
	if value := strings.TrimSpace(p.DatabaseTable); value != "" {
		return value
	}
	return "billing_ledger"
}
