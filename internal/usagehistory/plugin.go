package usagehistory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

const timestampLayout = "2006-01-02T15:04:05.000000000Z"

type Plugin struct {
	Store        *Store
	AccountLabel func(string) string
}

// HandleUsage stores metadata only; credentials, response headers and raw bodies are excluded.
// This sink is independent of the optional transient usage queue toggle.
func (p *Plugin) HandleUsage(ctx context.Context, r usage.Record) {
	if p == nil || p.Store == nil {
		return
	}
	meta := logging.GetClientRequestMetadata(ctx)
	requested := r.RequestedAt
	if requested.IsZero() {
		requested = time.Now()
	}
	account := r.AuthID
	if p.AccountLabel != nil {
		if label := p.AccountLabel(r.AuthID); label != "" {
			account = label
		}
	}
	if account == "" {
		account = r.AuthIndex
	}
	accountID := r.AuthID
	if accountID == "" {
		accountID = r.AuthIndex
	}
	keyID := ""
	if r.APIKey != "" {
		sum := sha256.Sum256([]byte(r.APIKey))
		keyID = hex.EncodeToString(sum[:])[:16]
	}
	detail := usage.EnsureTokenBreakdownForProvider(r.Detail, r.Provider, r.ExecutorType).TokenBreakdown
	status := r.Fail.StatusCode
	if status == 0 {
		status = logging.GetResponseStatus(ctx)
	}
	failed := r.Failed || status >= 400
	if status == 0 {
		if failed {
			status = 500
		} else {
			status = 200
		}
	}
	endpoint := logging.GetEndpoint(ctx)
	if parsed, err := url.Parse(endpoint); err == nil {
		endpoint = parsed.Path
	}
	failure := ""
	if failed {
		failure = "HTTP request failed"
	}
	record := Record{
		Timestamp: requested.UTC().Format(timestampLayout), RequestID: logging.GetRequestID(ctx), SessionID: r.SessionID,
		Provider: r.Provider, Model: r.Model, Alias: r.Alias, Account: filepath.Base(account), AccountID: accountID,
		APIKeyID: keyID, Endpoint: endpoint, ClientIP: meta.ClientIP, UserAgent: meta.UserAgent,
		ReasoningEffort: r.ReasoningEffort, ServiceTier: r.ServiceTier, Stream: r.Stream || usage.StreamFromContext(ctx),
		Failed: failed, StatusCode: status, Error: failure, InputTokens: detail.Input.UncachedTokens,
		OutputTokens: detail.Output.TotalTokens, CacheReadTokens: detail.Input.CacheReadTokens,
		CacheWriteTokens: detail.Input.CacheWriteTokens, ReasoningTokens: detail.Output.ReasoningTokens,
		CacheWrite5mTokens: r.Detail.CacheCreation5mTokens, CacheWrite1hTokens: r.Detail.CacheCreation1hTokens,
		TotalTokens: detail.TotalTokens, AccountingQuality: string(detail.Quality), LatencyMs: r.Latency.Milliseconds(), TTFTMs: r.TTFT.Milliseconds(),
		AccountStatsCost: r.AccountStatsCost, AccountRateMultiplier: r.AccountRateMultiplier,
	}
	if record.SessionID == "" {
		record.SessionID = meta.SessionID
	}
	if record.ReasoningEffort == "" {
		record.ReasoningEffort = usage.ReasoningEffortFromContext(ctx)
	}
	if record.ServiceTier == "" {
		record.ServiceTier = usage.ServiceTierFromContext(ctx)
	}
	record.Model = strings.TrimSpace(record.Model)
	if record.Provider == "claude" || record.CacheWrite5mTokens > 0 || record.CacheWrite1hTokens > 0 {
		record.CacheWrite5mTokens, record.CacheWrite1hTokens = usage.NormalizeCacheCreationBreakdown(record.CacheWriteTokens, record.CacheWrite5mTokens, record.CacheWrite1hTokens)
	}
	if err := p.Store.Append(context.Background(), record); err != nil {
		p.Store.writeErrors.Add(1)
		log.Errorf("usage history write failed: %v", err)
	}
}
