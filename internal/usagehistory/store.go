// Package usagehistory persists request usage independently of the consumable usage queue.
package usagehistory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

type Record struct {
	ID                    int64    `json:"id"`
	Timestamp             string   `json:"timestamp"`
	RequestID             string   `json:"request_id"`
	SessionID             string   `json:"session_id"`
	Provider              string   `json:"provider"`
	Model                 string   `json:"model"`
	Alias                 string   `json:"alias"`
	Account               string   `json:"account"`
	AccountID             string   `json:"account_id"`
	APIKeyID              string   `json:"api_key_id"`
	Endpoint              string   `json:"endpoint"`
	ClientIP              string   `json:"client_ip"`
	UserAgent             string   `json:"user_agent"`
	ReasoningEffort       string   `json:"reasoning_effort"`
	ServiceTier           string   `json:"service_tier"`
	Stream                bool     `json:"stream"`
	Failed                bool     `json:"failed"`
	StatusCode            int      `json:"status_code"`
	Error                 string   `json:"error"`
	InputTokens           int64    `json:"input_tokens"`
	OutputTokens          int64    `json:"output_tokens"`
	CacheReadTokens       int64    `json:"cache_read_tokens"`
	CacheWriteTokens      int64    `json:"cache_write_tokens"`
	CacheWrite5mTokens    int64    `json:"cache_write_5m_tokens,omitempty"`
	CacheWrite1hTokens    int64    `json:"cache_write_1h_tokens,omitempty"`
	ReasoningTokens       int64    `json:"reasoning_tokens"`
	TotalTokens           int64    `json:"total_tokens"`
	AccountingQuality     string   `json:"accounting_quality"`
	LatencyMs             int64    `json:"latency_ms"`
	TTFTMs                int64    `json:"ttft_ms"`
	Cost                  *float64 `json:"cost"`
	AccountStatsCost      *float64 `json:"account_stats_cost,omitempty"`
	AccountRateMultiplier *float64 `json:"account_rate_multiplier,omitempty"`
	Price                 *Price   `json:"price"`
}

// Price is a USD-per-million-token price snapshot. Output includes reasoning.
type Price struct {
	Model      string  `json:"model"`
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	// Nil preserves legacy overrides that apply one cache-write price to both TTLs.
	CacheWrite1h *float64 `json:"cache_write_1h,omitempty"`
}

type Store struct {
	db          *sql.DB
	writeErrors atomic.Int64
}

var active atomic.Pointer[Store]

func Active() *Store                { return active.Load() }
func SetActive(s *Store)            { active.Store(s) }
func (s *Store) WriteErrors() int64 { return s.writeErrors.Load() }
func (s *Store) Close() error       { active.CompareAndSwap(s, nil); return s.db.Close() }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS usage_records (
 id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp TEXT NOT NULL, provider TEXT NOT NULL,
 model TEXT NOT NULL, account_id TEXT NOT NULL, api_key_id TEXT NOT NULL,
 failed INTEGER NOT NULL, stream INTEGER NOT NULL, status_code INTEGER NOT NULL,
 total_tokens INTEGER NOT NULL, input_tokens INTEGER NOT NULL, output_tokens INTEGER NOT NULL,
 cache_read_tokens INTEGER NOT NULL, cache_write_tokens INTEGER NOT NULL,
 latency_ms INTEGER NOT NULL, ttft_ms INTEGER NOT NULL, cost REAL, payload TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS usage_time ON usage_records(timestamp,id);
 CREATE INDEX IF NOT EXISTS usage_account_time ON usage_records(account_id,timestamp);
 CREATE INDEX IF NOT EXISTS usage_key_time ON usage_records(api_key_id,timestamp);
 CREATE INDEX IF NOT EXISTS usage_model_time ON usage_records(model,timestamp);
 CREATE TABLE IF NOT EXISTS usage_prices(model TEXT PRIMARY KEY, payload TEXT NOT NULL);`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Append(ctx context.Context, r Record) error {
	for _, value := range []*float64{r.AccountStatsCost, r.AccountRateMultiplier} {
		if value != nil && (*value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0)) {
			return fmt.Errorf("invalid account billing snapshot")
		}
	}
	parsed, errTime := time.Parse(time.RFC3339Nano, r.Timestamp)
	if errTime != nil {
		return errTime
	}
	r.Timestamp = parsed.UTC().Format(timestampLayout)
	var raw string
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM usage_prices WHERE model=?", r.Model).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var price *Price
	if err == nil {
		var p Price
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return err
		}
		price = &p
	} else {
		price = defaultPrice(r)
	}
	if r.Cost == nil {
		r.Cost = tokenCost(r, price)
		if r.Cost != nil {
			r.Price = price
		}
	}
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO usage_records(timestamp,provider,model,account_id,api_key_id,failed,stream,status_code,total_tokens,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,latency_ms,ttft_ms,cost,payload) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.Timestamp, r.Provider, r.Model, r.AccountID, r.APIKeyID, r.Failed, r.Stream, r.StatusCode, r.TotalTokens, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.LatencyMs, r.TTFTMs, r.Cost, string(body))
	return err
}

type Filter struct {
	Start       string `json:"start"`
	End         string `json:"end"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	AccountID   string `json:"account_id"`
	APIKeyID    string `json:"api_key_id"`
	Status      string `json:"status"`
	RequestType string `json:"request_type"`
	Search      string `json:"search"`
	StatusCode  int    `json:"status_code"`
	Page        int    `json:"page"`
	PageSize    int    `json:"page_size"`
	Sort        string `json:"sort"`
	Order       string `json:"order"`
	Snapshot    int64  `json:"snapshot"`
}

func (f *Filter) Validate() error {
	for _, value := range []string{f.Start, f.End} {
		if value != "" {
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
				return fmt.Errorf("dates must be RFC3339 timestamps")
			}
		}
	}
	if f.Start != "" {
		t, _ := time.Parse(time.RFC3339Nano, f.Start)
		f.Start = t.UTC().Format(timestampLayout)
	}
	if f.End != "" {
		t, _ := time.Parse(time.RFC3339Nano, f.End)
		f.End = t.UTC().Format(timestampLayout)
	}
	if f.Start != "" && f.End != "" && f.Start >= f.End {
		return fmt.Errorf("start must precede end")
	}
	if f.Status != "" && f.Status != "success" && f.Status != "failed" {
		return fmt.Errorf("invalid status")
	}
	if f.RequestType != "" && f.RequestType != "stream" && f.RequestType != "non_stream" {
		return fmt.Errorf("invalid request_type")
	}
	if f.StatusCode != 0 && (f.StatusCode < 100 || f.StatusCode > 599) {
		return fmt.Errorf("invalid HTTP status")
	}
	if len(f.Search) > 200 {
		return fmt.Errorf("search is too long")
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Page > 1000000 {
		return fmt.Errorf("page too large")
	}
	if f.PageSize < 1 {
		f.PageSize = 25
	}
	if f.PageSize > 1000 {
		return fmt.Errorf("page_size exceeds 1000")
	}
	if f.Sort == "" {
		f.Sort = "timestamp"
	}
	if !strings.Contains("|timestamp|model|total_tokens|cost|latency_ms|ttft_ms|", "|"+f.Sort+"|") {
		return fmt.Errorf("invalid sort")
	}
	if f.Order == "" {
		f.Order = "desc"
	}
	if f.Order != "asc" && f.Order != "desc" {
		return fmt.Errorf("invalid order")
	}
	if f.Snapshot < 0 {
		return fmt.Errorf("invalid snapshot")
	}
	return nil
}
func (f Filter) where() (string, []any) {
	parts := []string{"1=1"}
	args := []any{}
	add := func(sql string, v any) { parts = append(parts, sql); args = append(args, v) }
	if f.Start != "" {
		add("timestamp>=?", f.Start)
	}
	if f.End != "" {
		add("timestamp<?", f.End)
	}
	for _, p := range [][2]string{{"provider", f.Provider}, {"model", f.Model}, {"account_id", f.AccountID}, {"api_key_id", f.APIKeyID}} {
		if p[1] != "" {
			add(p[0]+"=?", p[1])
		}
	}
	if f.Status != "" {
		add("failed=?", f.Status == "failed")
	}
	if f.RequestType != "" {
		add("stream=?", f.RequestType == "stream")
	}
	if f.StatusCode != 0 {
		add("status_code=?", f.StatusCode)
	}
	if f.Snapshot > 0 {
		add("id<=?", f.Snapshot)
	}
	if f.Search != "" {
		add("(instr(lower(payload),lower(?))>0)", f.Search)
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

type Stats struct {
	Requests   int64   `json:"requests"`
	Failed     int64   `json:"failed"`
	Tokens     int64   `json:"tokens"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cache_read"`
	CacheWrite int64   `json:"cache_write"`
	Cost       float64 `json:"cost"`
	Unpriced   int64   `json:"unpriced"`
	Latency    float64 `json:"latency"`
	TTFT       float64 `json:"ttft"`
}
type Group struct {
	Name     string  `json:"name"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
}
type Result struct {
	Items       []Record `json:"items"`
	Total       int64    `json:"total"`
	Page        int      `json:"page"`
	PageSize    int      `json:"page_size"`
	Snapshot    int64    `json:"snapshot"`
	Stats       Stats    `json:"stats"`
	Models      []Group  `json:"models"`
	Accounts    []Group  `json:"accounts"`
	Trend       []Group  `json:"trend"`
	WriteErrors int64    `json:"write_errors"`
}

func (s *Store) Query(ctx context.Context, f Filter) (Result, error) {
	if err := f.Validate(); err != nil {
		return Result{}, err
	}
	result := Result{Items: []Record{}, Page: f.Page, PageSize: f.PageSize, WriteErrors: s.WriteErrors()}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if f.Snapshot == 0 {
		if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(id),0) FROM usage_records").Scan(&f.Snapshot); err != nil {
			return result, err
		}
	}
	result.Snapshot = f.Snapshot
	where, args := f.where()
	err = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(failed),0),coalesce(sum(total_tokens),0),coalesce(sum(input_tokens),0),coalesce(sum(output_tokens),0),coalesce(sum(cache_read_tokens),0),coalesce(sum(cache_write_tokens),0),coalesce(sum(cost),0),coalesce(sum(cost IS NULL),0),coalesce(avg(latency_ms),0),coalesce(avg(ttft_ms),0) FROM usage_records`+where, args...).Scan(&result.Stats.Requests, &result.Stats.Failed, &result.Stats.Tokens, &result.Stats.Input, &result.Stats.Output, &result.Stats.CacheRead, &result.Stats.CacheWrite, &result.Stats.Cost, &result.Stats.Unpriced, &result.Stats.Latency, &result.Stats.TTFT)
	if err != nil {
		return result, err
	}
	result.Total = result.Stats.Requests
	rows, err := tx.QueryContext(ctx, "SELECT id,payload FROM usage_records"+where+" ORDER BY "+f.Sort+" "+f.Order+", id "+f.Order+" LIMIT ? OFFSET ?", append(append([]any{}, args...), f.PageSize, (f.Page-1)*f.PageSize)...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var r Record
		var id int64
		var raw string
		if err = rows.Scan(&id, &raw); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			break
		}
		r.ID = id
		result.Items = append(result.Items, r)
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if err != nil {
		return result, err
	}
	if rowsErr != nil {
		return result, rowsErr
	}
	group := func(column, order string) ([]Group, error) {
		out := []Group{}
		rows, e := tx.QueryContext(ctx, "SELECT "+column+",count(*),coalesce(sum(total_tokens),0),coalesce(sum(cost),0) FROM usage_records"+where+" GROUP BY 1 ORDER BY "+order+" LIMIT 60", args...)
		if e != nil {
			return out, e
		}
		defer rows.Close()
		for rows.Next() {
			var g Group
			if e = rows.Scan(&g.Name, &g.Requests, &g.Tokens, &g.Cost); e != nil {
				return out, e
			}
			out = append(out, g)
		}
		return out, rows.Err()
	}
	if result.Models, err = group("model", "2 DESC"); err != nil {
		return result, err
	}
	if result.Accounts, err = group("json_extract(payload,'$.account')", "2 DESC"); err != nil {
		return result, err
	}
	if result.Trend, err = group("substr(timestamp,1,10)", "1 DESC"); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

type Options struct {
	Providers []string        `json:"providers"`
	Models    []string        `json:"models"`
	Accounts  []AccountOption `json:"accounts"`
	APIKeys   []string        `json:"api_keys"`
}
type AccountOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Store) Options(ctx context.Context) (Options, error) {
	o := Options{Providers: []string{}, Models: []string{}, Accounts: []AccountOption{}, APIKeys: []string{}}
	for _, spec := range []struct {
		column string
		dest   *[]string
	}{{"provider", &o.Providers}, {"model", &o.Models}, {"api_key_id", &o.APIKeys}} {
		rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT "+spec.column+" FROM usage_records WHERE "+spec.column+"<>'' ORDER BY 1")
		if err != nil {
			return o, err
		}
		for rows.Next() {
			var v string
			if err = rows.Scan(&v); err != nil {
				break
			}
			*spec.dest = append(*spec.dest, v)
		}
		e := rows.Err()
		rows.Close()
		if err != nil {
			return o, err
		}
		if e != nil {
			return o, e
		}
	}
	rows, err := s.db.QueryContext(ctx, "SELECT account_id, max(json_extract(payload,'$.account')) FROM usage_records GROUP BY account_id")
	if err != nil {
		return o, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err = rows.Scan(&id, &name); err != nil {
			return o, err
		}
		o.Accounts = append(o.Accounts, AccountOption{ID: id, Name: name})
	}
	return o, rows.Err()
}
func (s *Store) Prices(ctx context.Context) ([]Price, error) {
	out := []Price{}
	rows, err := s.db.QueryContext(ctx, "SELECT payload FROM usage_prices ORDER BY model")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var p Price
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return out, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) SavePrices(ctx context.Context, prices []Price) error {
	if len(prices) > 1000 {
		return fmt.Errorf("too many prices")
	}
	seen := map[string]bool{}
	for _, p := range prices {
		if p.Model == "" || len(p.Model) > 200 || seen[p.Model] {
			return fmt.Errorf("model must be nonempty and unique")
		}
		seen[p.Model] = true
		rates := []float64{p.Input, p.Output, p.CacheRead, p.CacheWrite}
		if p.CacheWrite1h != nil {
			rates = append(rates, *p.CacheWrite1h)
		}
		for _, n := range rates {
			if n < 0 || n > 1e6 || math.IsNaN(n) || math.IsInf(n, 0) {
				return fmt.Errorf("invalid price")
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM usage_prices"); err != nil {
		return err
	}
	for _, p := range prices {
		raw, _ := json.Marshal(p)
		if _, err = tx.ExecContext(ctx, "INSERT INTO usage_prices(model,payload) VALUES(?,?)", p.Model, string(raw)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) Delete(ctx context.Context, f Filter) (int64, error) {
	if err := f.Validate(); err != nil {
		return 0, err
	}
	if f.Start == "" || f.End == "" {
		return 0, fmt.Errorf("cleanup requires an explicit date range")
	}
	where, args := f.where()
	r, err := s.db.ExecContext(ctx, "DELETE FROM usage_records"+where, args...)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}
