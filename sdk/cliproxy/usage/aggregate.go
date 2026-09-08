package usage

import (
	"sort"
	"strings"
	"sync"
)

// AccountTotal is the cumulative token and monetary usage for one account.
// Account is derived from AuthID, APIKey, or the access-token hash (in that order).
type AccountTotal struct {
	Account      string  `json:"account"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheRead    int64   `json:"cache_read_tokens"`
	CacheWrite   int64   `json:"cache_write_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalMicros  int64   `json:"total_micros"`
	TotalUSD     float64 `json:"total_usd"`
}

// BillingTotals aggregates successfully applied charges in memory. It is a
// read model for management APIs; the SQL ledger remains the source of truth.
type BillingTotals struct {
	mu       sync.RWMutex
	accounts map[string]AccountTotal
	seen     map[string]struct{}
}

func NewBillingTotals() *BillingTotals {
	return &BillingTotals{accounts: make(map[string]AccountTotal), seen: make(map[string]struct{})}
}

func accountKey(c Charge) string {
	for _, value := range []string{c.Record.AuthID, c.APIKey, c.Record.AccessTokenSHA256} {
		if v := strings.TrimSpace(value); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(c.Record.Provider); v != "" {
		return v + ":unknown"
	}
	return "unknown"
}

// Add records a charge once, making retries and duplicate callbacks harmless.
func (a *BillingTotals) Add(c Charge) {
	if a == nil || strings.TrimSpace(c.EventID) == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.seen[c.EventID]; ok {
		return
	}
	a.seen[c.EventID] = struct{}{}
	key := accountKey(c)
	t := a.accounts[key]
	t.Account = key
	t.Requests++
	t.InputTokens += c.Quote.InputTokens
	t.OutputTokens += c.Quote.OutputTokens
	t.CacheRead += c.Quote.CacheReadTokens
	t.CacheWrite += c.Quote.CacheWriteTokens
	t.TotalTokens += c.Quote.InputTokens + c.Quote.OutputTokens + c.Quote.CacheReadTokens + c.Quote.CacheWriteTokens
	t.TotalMicros += c.Quote.TotalMicros
	t.TotalUSD += c.Quote.TotalUSD
	a.accounts[key] = t
}

// Snapshot returns account totals and the global aggregate (Account="total").
func (a *BillingTotals) Snapshot() []AccountTotal {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]AccountTotal, 0, len(a.accounts)+1)
	var total AccountTotal
	total.Account = "total"
	for _, t := range a.accounts {
		out = append(out, t)
		total.Requests += t.Requests
		total.InputTokens += t.InputTokens
		total.OutputTokens += t.OutputTokens
		total.CacheRead += t.CacheRead
		total.CacheWrite += t.CacheWrite
		total.TotalTokens += t.TotalTokens
		total.TotalMicros += t.TotalMicros
		total.TotalUSD += t.TotalUSD
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	return append(out, total)
}

func (a *BillingTotals) Reset() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.accounts = make(map[string]AccountTotal)
	a.seen = make(map[string]struct{})
}

var defaultBillingTotals = NewBillingTotals()

func DefaultBillingTotals() *BillingTotals { return defaultBillingTotals }
