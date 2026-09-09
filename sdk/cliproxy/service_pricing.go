package cliproxy

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	log "github.com/sirupsen/logrus"
)

const pricingPluginName = "cliproxy:pricing"

var pricingOwner struct {
	sync.Mutex
	service *Service
}

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
// configuration. Durable mode reads a PostgreSQL DSN from the configured
// environment variable; in-memory mode is intended for previews and tests.
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
	pricingOwner.Lock()
	defer pricingOwner.Unlock()
	s.pricingMu.Lock()
	defer s.pricingMu.Unlock()
	if cfg.Pricing.Enabled && pricingOwner.service != nil && pricingOwner.service != s {
		return fmt.Errorf("pricing plugin is already owned by another service")
	}
	if !cfg.Pricing.Enabled {
		if s.pricingDB != nil {
			_ = s.pricingDB.Close()
			s.pricingDB = nil
		}
		s.pricingSink = nil
		s.pricingTotals = nil
		if pricingOwner.service == s {
			usage.RegisterNamedPlugin(pricingPluginName, disabledPricingPlugin{})
			pricingOwner.service = nil
		}
		return nil
	}
	var db *sql.DB
	var sink usage.ChargeSink
	if cfg.Pricing.InMemory {
		sink, _ = s.pricingSink.(*usage.MemoryChargeSink)
		if memorySink, ok := sink.(*usage.MemoryChargeSink); !ok || memorySink == nil {
			sink = usage.NewMemoryChargeSink()
		}
	} else {
		dsn := strings.TrimSpace(os.Getenv(cfg.Pricing.DSNEnv()))
		if dsn == "" {
			return fmt.Errorf("pricing database DSN environment variable %q is empty", cfg.Pricing.DSNEnv())
		}
		var err error
		db, err = sql.Open("pgx", dsn)
		if err != nil {
			return fmt.Errorf("open pricing database: %w", err)
		}
		if err := db.PingContext(ctx); err != nil {
			_ = db.Close()
			return fmt.Errorf("open pricing database: %w", err)
		}
		sqlSink := usage.NewSQLChargeSink(db, cfg.Pricing.TableName())
		if err := sqlSink.EnsureSchema(ctx); err != nil {
			_ = db.Close()
			return err
		}
		sink = sqlSink
	}
	engine := usage.NewPriceEngine(cfg.Pricing.PricingTable)
	// Keep the read model scoped to this service. Durable history remains in SQL;
	// a process-wide singleton would let multiple Service instances reset or mix
	// each other's in-memory totals.
	totals := usage.NewBillingTotals()
	if _, ok := s.pricingSink.(*usage.MemoryChargeSink); cfg.Pricing.InMemory && ok && s.pricingTotals != nil {
		totals = s.pricingTotals
	}
	plugin := &usage.PricingPlugin{Engine: engine, Sink: sink, Totals: totals, RateMultiplier: cfg.Pricing.RateMultiplier,
		OnError: func(err error) { log.WithError(err).Error("pricing charge failed") }}
	if s.pricingDB != nil {
		_ = s.pricingDB.Close()
	}
	pricingOwner.service = s
	usage.RegisterNamedPlugin(pricingPluginName, plugin)
	s.pricingDB = db
	s.pricingSink = sink
	s.pricingTotals = totals
	return nil
}

func (s *Service) closePricing() {
	if s == nil {
		return
	}
	pricingOwner.Lock()
	defer pricingOwner.Unlock()
	s.pricingMu.Lock()
	defer s.pricingMu.Unlock()
	if s.pricingDB != nil {
		_ = s.pricingDB.Close()
		s.pricingDB = nil
	}
	s.pricingSink = nil
	s.pricingTotals = nil
	if pricingOwner.service == s {
		usage.RegisterNamedPlugin(pricingPluginName, disabledPricingPlugin{})
		pricingOwner.service = nil
	}
}
