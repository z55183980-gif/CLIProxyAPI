package helps

// Request translation entry point shared by the Claude and OpenAI-compatible
// executors.
//
// This replaces the former codex_multi_agent_v2.go. The Codex multi-agent
// namespace optimization and orphan-delegation rewrites lived there and applied
// only to Codex and Interactions upstreams, both of which are gone, so the
// compatibility branches that targeted them were dropped with them. The
// signature is unchanged so existing call sites keep working.

import (
	"context"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/constant"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	openaiClaude "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/claude"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

// TranslateRequestWithAPIKeyModelCompatibility translates a client request into
// the upstream wire format and then applies the request's thinking summary
// configuration for the target model.
//
// ctx, headers, cfg, and isCompat are retained for call-site compatibility.
func TranslateRequestWithAPIKeyModelCompatibility(_ context.Context, _ http.Header, _ *config.Config, from, to sdktranslator.Format, model string, payload []byte, stream, isCompat bool) []byte {
	var translated []byte
	// OpenAI-compatible gateways may accept unsigned/legacy Claude thinking
	// blocks; preserve those blocks when translating Claude requests.
	if isCompat && from.String() == constant.Claude && to.String() == constant.OpenAI {
		translated = openaiClaude.ConvertClaudeRequestToOpenAIWithCompat(model, payload, stream)
	} else {
		translated = sdktranslator.TranslateRequest(from, to, model, payload, stream)
	}
	summaryConfig := thinking.ExtractSummaryConfig(payload, from.String())
	return thinking.ApplySummaryConfigForModel(translated, to.String(), model, summaryConfig)
}
