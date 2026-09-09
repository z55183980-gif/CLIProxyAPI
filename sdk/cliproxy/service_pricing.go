package cliproxy

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	log "github.com/sirupsen/logrus"
)

const pricingPluginName = "cliproxy:pricing"

// PricingTotals returns the in-memory aggregate maintained by the pricing
// plugin. It is intended for management/UI read models; durable history lives
// in the SQL ledger.
func (s *Service) PricingTotals() *usage.BillingTotals {
	if s == nil {
		return nil
	}
	s.pricingMu.Lock()
	defer s.pricingMu.Unlock()
	return s.pricingTotals
}

type disabledPricingPlugin struct{}

func (disabledPricingPlugin) HandleUsage(context.Context, usage.Record) {}

// configurePricing applies the optional pricing plugin for the supplied
// configuration. Durable mode uses PostgreSQL, while in-memory mode is
// available for local previews and development when no database is present.
// The DSN itself is never copied into Config or logs.
func (s *Service) configurePricing(ctx context.Context, cfg *config.Config) error {
	if s == nil || cfg == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := cfg.Pricing.Validate(); err != nil {
		return err
	}
	if !cfg.Pricing.Enabled {
		s.pricingMu.Lock()
		if s.pricingDB != nil {
			_ = s.pricingDB.Close()
			s.pricingDB = nil
		}
		s.pricingSink = nil
		s.pricingTotals = nil
		usage.RegisterNamedPlugin(pricingPluginName, disabledPricingPlugin{})
		s.pricingMu.Unlock()
		return nil
	}
	if cfg.Pricing.InMemory {
		// Keep charges process-local; this mode intentionally has no durable
		// history and is suitable for previews where PostgreSQL is unavailable.
		sink := usage.NewMemoryChargeSink()
		engine := usage.NewPriceEngine(cfg.Pricing.PricingTable)
		totals := usage.DefaultBillingTotals()
		plugin := &usage.PricingPlugin{Engine: engine, Sink: sink, Totals: totals, RateMultiplier: cfg.Pricing.RateMultiplier,
			OnError: func(err error) { log.WithError(err).Error("pricing charge failed") }}
		s.pricingMu.Lock()
		if s.pricingDB != nil {
			_ = s.pricingDB.Close()
			s.pricingDB = nil
		}
		usage.RegisterNamedPlugin(pricingPluginName, plugin)
		s.pricingSink = sink
		s.pricingTotals = totals
		s.pricingMu.Unlock()
		return nil
	}
	dsn := strings.TrimSpace(os.Getenv(cfg.Pricing.DSNEnv()))
	if dsn == "" {
		return fmt.Errorf("pricing database DSN environment variable %q is empty", cfg.Pricing.DSNEnv())
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open pricing database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("open pricing database: %w", err)
	}
	sink := usage.NewSQLChargeSink(db, cfg.Pricing.TableName())
	if err := sink.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		return err
	}
	engine := usage.NewPriceEngine(cfg.Pricing.PricingTable)
	// Keep a process-wide read model so management handlers can expose totals
	// without depending on the service instance.
	totals := usage.DefaultBillingTotals()
	plugin := &usage.PricingPlugin{Engine: engine, Sink: sink, Totals: totals, RateMultiplier: cfg.Pricing.RateMultiplier,
		OnError: func(err error) { log.WithError(err).Error("pricing charge failed") }}
	s.pricingMu.Lock()
	if s.pricingDB != nil {
		_ = s.pricingDB.Close()
	}
	usage.RegisterNamedPlugin(pricingPluginName, plugin)
	s.pricingDB = db
	s.pricingSink = sink
	s.pricingTotals = totals
	s.pricingMu.Unlock()
	return nil
}

func (s *Service) closePricing() {
	if s == nil {
		return
	}
	s.pricingMu.Lock()
	defer s.pricingMu.Unlock()
	if s.pricingDB != nil {
		_ = s.pricingDB.Close()
		s.pricingDB = nil
	}
	s.pricingSink = nil
	s.pricingTotals = nil
}
