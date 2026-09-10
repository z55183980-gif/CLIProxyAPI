package usagehistory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// AccountStats contains totals for one account in a bounded calendar window.
type AccountStats struct {
	Requests  int64   `json:"requests"`
	Tokens    int64   `json:"tokens"`
	Cost      float64 `json:"cost"`
	Unpriced  int64   `json:"unpriced"`
	Estimated int64   `json:"estimated"`
}

// AccountWindowStats batches the visible accounts, including accounts with no traffic.
// Existing price snapshots are preserved; previously unpriced records may be estimated.
func (s *Store) AccountWindowStats(ctx context.Context, ids []string, start, end string) (map[string]AccountStats, error) {
	f := Filter{Start: start, End: end}
	if start == "" || end == "" {
		return nil, fmt.Errorf("a bounded date range is required")
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	if len(ids) > 200 {
		return nil, fmt.Errorf("at most 200 account IDs are allowed")
	}
	result := make(map[string]AccountStats, len(ids))
	args := []any{f.Start, f.End}
	marks := []string{}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || len(id) > 1024 {
			return nil, fmt.Errorf("invalid account ID")
		}
		if _, exists := result[id]; exists {
			continue
		}
		result[id] = AccountStats{}
		args = append(args, id)
		marks = append(marks, "?")
	}
	if len(marks) == 0 {
		return result, nil
	}
	prices, err := s.Prices(ctx)
	if err != nil {
		return nil, err
	}
	overrides := make(map[string]Price, len(prices))
	for _, p := range prices {
		overrides[p.Model] = p
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	where := " WHERE timestamp>=? AND timestamp<? AND account_id IN (" + strings.Join(marks, ",") + ")"
	rows, err := tx.QueryContext(ctx, `SELECT account_id,count(*),coalesce(sum(total_tokens),0),coalesce(sum(coalesce(json_extract(payload,'$.account_stats_cost'),cost)*coalesce(json_extract(payload,'$.account_rate_multiplier'),1)),0),coalesce(sum(cost IS NULL AND json_extract(payload,'$.account_stats_cost') IS NULL),0) FROM usage_records`+where+" GROUP BY account_id", args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var stats AccountStats
		if err = rows.Scan(&id, &stats.Requests, &stats.Tokens, &stats.Cost, &stats.Unpriced); err != nil {
			break
		}
		result[id] = stats
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if rowsErr != nil {
		return nil, rowsErr
	}
	rows, err = tx.QueryContext(ctx, "SELECT payload FROM usage_records"+where+" AND cost IS NULL AND json_extract(payload,'$.account_stats_cost') IS NULL", args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		var record Record
		if err = rows.Scan(&raw); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(raw), &record); err != nil {
			break
		}
		var price *Price
		if p, ok := overrides[record.Model]; ok {
			price = &p
		} else {
			price = defaultPrice(record)
		}
		if cost := tokenCost(record, price); cost != nil {
			stats := result[record.AccountID]
			multiplier := 1.0
			if record.AccountRateMultiplier != nil {
				multiplier = *record.AccountRateMultiplier
			}
			stats.Cost += *cost * multiplier
			stats.Unpriced--
			stats.Estimated++
			result[record.AccountID] = stats
		}
	}
	rowsErr = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if rowsErr != nil {
		return nil, rowsErr
	}
	return result, tx.Commit()
}
