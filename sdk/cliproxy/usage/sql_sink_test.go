package usage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
)

// Exercise database/sql's transaction lifecycle without requiring an external
// PostgreSQL service. SQL dialect/concurrency semantics still require a live
// PostgreSQL integration test.
type ledgerTestConnector struct {
	inserted                    bool
	fingerprint                 string
	queries, commits, rollbacks int
}

func (c *ledgerTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &ledgerTestConn{c}, nil
}
func (c *ledgerTestConnector) Driver() driver.Driver { return ledgerTestDriver{} }

type ledgerTestDriver struct{}

func (ledgerTestDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type ledgerTestConn struct{ state *ledgerTestConnector }

func (c *ledgerTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *ledgerTestConn) Close() error              { return nil }
func (c *ledgerTestConn) Begin() (driver.Tx, error) { return &ledgerTestTx{c.state}, nil }
func (c *ledgerTestConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c *ledgerTestConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.queries++
	switch c.state.queries {
	case 1:
		if c.state.inserted {
			return &ledgerTestRows{value: "event"}, nil
		}
		return &ledgerTestRows{done: true}, nil
	case 2:
		return &ledgerTestRows{value: c.state.fingerprint}, nil
	default:
		return nil, errors.New("unexpected query")
	}
}

type ledgerTestTx struct{ state *ledgerTestConnector }

func (t *ledgerTestTx) Commit() error   { t.state.commits++; return nil }
func (t *ledgerTestTx) Rollback() error { t.state.rollbacks++; return nil }

type ledgerTestRows struct {
	value string
	done  bool
}

func (r *ledgerTestRows) Columns() []string { return []string{"value"} }
func (r *ledgerTestRows) Close() error      { return nil }
func (r *ledgerTestRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.value
	return nil
}

func TestSQLChargeSinkCommitsOnlyNewEventHooksAndRollsBackFailures(t *testing.T) {
	hookErr := errors.New("hook failed")
	for _, tc := range []struct {
		name                                  string
		inserted                              bool
		fingerprint                           string
		hookErr                               error
		wantErr                               error
		wantHooks, wantCommits, wantRollbacks int
	}{
		{"insert", true, "f", nil, nil, 1, 1, 0},
		{"replay", false, "f", nil, nil, 0, 1, 0},
		{"conflict", false, "other", nil, ErrChargeConflict, 0, 0, 1},
		{"hook failure", true, "f", hookErr, hookErr, 1, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &ledgerTestConnector{inserted: tc.inserted, fingerprint: tc.fingerprint}
			db := sql.OpenDB(state)
			defer db.Close()
			sink := NewSQLChargeSink(db, "ledger")
			hooks := 0
			sink.OnInsert = func(context.Context, *sql.Tx, Charge) error { hooks++; return tc.hookErr }
			err := sink.Apply(nil, Charge{EventID: "event", Fingerprint: "f"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Apply = %v, want %v", err, tc.wantErr)
			}
			if hooks != tc.wantHooks || state.commits != tc.wantCommits || state.rollbacks != tc.wantRollbacks {
				t.Fatalf("hooks=%d commits=%d rollbacks=%d", hooks, state.commits, state.rollbacks)
			}
		})
	}
}

func TestSQLChargeSinkRollsBackWhenHookPanics(t *testing.T) {
	state := &ledgerTestConnector{inserted: true, fingerprint: "f"}
	db := sql.OpenDB(state)
	defer db.Close()
	sink := NewSQLChargeSink(db, "ledger")
	sink.OnInsert = func(context.Context, *sql.Tx, Charge) error { panic("hook panic") }
	func() {
		defer func() {
			if recover() == nil {
				t.Error("hook did not panic")
			}
		}()
		_ = sink.Apply(context.Background(), Charge{EventID: "event", Fingerprint: "f"})
	}()
	if state.commits != 0 || state.rollbacks != 1 || db.Stats().InUse != 0 {
		t.Fatalf("transaction leaked: commits=%d rollbacks=%d in-use=%d", state.commits, state.rollbacks, db.Stats().InUse)
	}
}
