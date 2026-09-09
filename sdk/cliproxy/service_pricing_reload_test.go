package cliproxy

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

type reloadBlockingChargeSink struct {
	db      *sql.DB
	entered chan struct{}
	release chan struct{}
	applied chan error
}

func (s *reloadBlockingChargeSink) Apply(ctx context.Context, _ usage.Charge) error {
	close(s.entered)
	<-s.release
	err := s.db.PingContext(ctx)
	s.applied <- err
	return err
}

type reloadTestConnector struct{}

func (reloadTestConnector) Connect(context.Context) (driver.Conn, error) {
	return reloadTestConn{}, nil
}
func (reloadTestConnector) Driver() driver.Driver { return reloadTestDriver{} }

type reloadTestDriver struct{}

func (reloadTestDriver) Open(string) (driver.Conn, error) { return reloadTestConn{}, nil }

type reloadTestConn struct{}

func (reloadTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (reloadTestConn) Close() error               { return nil }
func (reloadTestConn) Begin() (driver.Tx, error)  { return nil, errors.New("unexpected transaction") }
func (reloadTestConn) Ping(context.Context) error { return nil }

func TestPricingReloadAndCloseWaitForInFlightLedgerWrite(t *testing.T) {
	for _, operation := range []string{"reload", "close", "disable"} {
		t.Run(operation, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Pricing.Enabled = true
			cfg.Pricing.InMemory = true
			cfg.Pricing.RateMultiplier = 1
			cfg.Pricing.PricingTable = usage.PricingTable{Revision: "test", Default: &usage.PriceCard{InputPerToken: 0.000001}}
			s := &Service{}
			if err := s.configurePricing(context.Background(), cfg); err != nil {
				t.Fatal(err)
			}
			db := sql.OpenDB(reloadTestConnector{})
			if err := db.Ping(); err != nil {
				t.Fatal(err)
			}
			blocked := &reloadBlockingChargeSink{db: db, entered: make(chan struct{}), release: make(chan struct{}), applied: make(chan error, 1)}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(blocked.release) }) }
			t.Cleanup(func() { release(); s.closePricing(); db.Close() })
			s.pricingMu.Lock()
			s.pricingDB = db
			s.pricingSink = blocked
			s.pricingPlugin.Sink = blocked
			s.pricingMu.Unlock()
			// Represent a dispatcher snapshot captured before reconfiguration.
			snapshot := servicePricingPlugin{service: s}
			record := usage.Record{RequestID: "before", Provider: "claude", Model: "claude-test", AuthID: "one-account",
				Detail: usage.Detail{TokenBreakdown: usage.NewSubsetTokenBreakdown(1, 0, 0, 0, 0, 1)}}
			writeDone := make(chan struct{})
			go func() { defer close(writeDone); snapshot.HandleUsage(context.Background(), record) }()
			select {
			case <-blocked.entered:
			case <-time.After(time.Second):
				t.Fatal("write did not enter sink")
			}
			changeDone := make(chan error, 1)
			go func() {
				if operation == "close" {
					s.closePricing()
					changeDone <- nil
					return
				}
				if operation == "disable" {
					cfg.Pricing.Enabled = false
				}
				changeDone <- s.configurePricing(context.Background(), cfg)
			}()
			select {
			case err := <-changeDone:
				t.Fatalf("%s completed before the write: %v", operation, err)
			case <-time.After(30 * time.Millisecond):
			}
			if err := db.Ping(); err != nil {
				t.Fatalf("old DB closed during Apply: %v", err)
			}
			release()
			select {
			case <-writeDone:
			case <-time.After(time.Second):
				t.Fatal("write did not finish")
			}
			if err := <-blocked.applied; err != nil {
				t.Fatalf("old sink failed after reload/close: %v", err)
			}
			select {
			case err := <-changeDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("reload/close did not finish")
			}
			if err := db.Ping(); err == nil {
				t.Fatal("old DB was not closed after Apply")
			}
			// A snapshot captured before replacement must resolve the new target,
			// or no target after disable/close, never call the retired SQL sink.
			record.RequestID = "after"
			snapshot.HandleUsage(context.Background(), record)
			if operation == "reload" {
				memory := s.pricingSink.(*usage.MemoryChargeSink)
				if len(memory.Charges()) != 1 {
					t.Fatalf("new target received %d charges", len(memory.Charges()))
				}
			}
		})
	}
}
