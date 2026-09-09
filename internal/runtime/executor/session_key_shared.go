package executor

// Shared session-identity resolution.
//
// These symbols were originally defined in codex_executor_reasoning.go and
// codex_websockets_request.go, but the Claude replay path and the
// OpenAI-compatible executor depend on them. They live here so those paths do
// not depend on any single provider's file surviving.
//
// Names are deliberately unchanged from their original definitions to keep the
// relocation a pure move with no call-site churn.

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

func sourceFormatEqual(from, want sdktranslator.Format) bool {
	return strings.EqualFold(strings.TrimSpace(from.String()), want.String())
}

func codexClaudeCodeReplaySessionKey(ctx context.Context, payload []byte, headers http.Header) string {
	sessionKey, _ := helps.ClaudeCodeExecutionScope(ctx, payload, headers)
	return sessionKey
}

func codexReasoningReplaySessionKey(ctx context.Context, from sdktranslator.Format, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, body []byte) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if sourceFormatEqual(from, sdktranslator.FormatClaude) {
		if sessionKey := codexClaudeCodeReplaySessionKey(ctx, req.Payload, opts.Headers); sessionKey != "" {
			return sessionKey
		}
	}
	if value := metadataString(opts.Metadata, cliproxyexecutor.ExecutionSessionMetadataKey); value != "" {
		return "execution:" + value
	}
	if value := metadataString(req.Metadata, cliproxyexecutor.ExecutionSessionMetadataKey); value != "" {
		return "execution:" + value
	}
	if value := codexReasoningReplaySessionKeyFromPayload(body); value != "" {
		return value
	}
	if value := codexReasoningReplaySessionKeyFromPayload(req.Payload); value != "" {
		return value
	}
	if value := codexReasoningReplaySessionKeyFromHeaders(opts.Headers); value != "" {
		return value
	}
	if ginCtx, ok := ctx.Value("gin").(*gin.Context); ok && ginCtx != nil && ginCtx.Request != nil {
		if value := codexReasoningReplaySessionKeyFromHeaders(ginCtx.Request.Header); value != "" {
			return value
		}
	}
	if sourceFormatEqual(from, sdktranslator.FormatOpenAI) {
		if apiKey := strings.TrimSpace(helps.APIKeyFromContext(ctx)); apiKey != "" {
			return "prompt-cache:" + uuid.NewSHA1(uuid.NameSpaceOID, []byte("cli-proxy-api:codex:prompt-cache:"+apiKey)).String()
		}
	}
	return ""
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		return ""
	}
}

func codexReasoningReplaySessionKeyFromPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	if promptCacheKey := strings.TrimSpace(gjson.GetBytes(payload, "prompt_cache_key").String()); promptCacheKey != "" {
		return "prompt-cache:" + promptCacheKey
	}
	if windowID := strings.TrimSpace(gjson.GetBytes(payload, "client_metadata.x-codex-window-id").String()); windowID != "" {
		return "window:" + windowID
	}
	if turnMetadata := strings.TrimSpace(gjson.GetBytes(payload, "client_metadata.x-codex-turn-metadata").String()); turnMetadata != "" {
		return codexReasoningReplaySessionKeyFromTurnMetadata(turnMetadata)
	}
	return ""
}

func codexReasoningReplaySessionKeyFromHeaders(headers http.Header) string {
	if headers == nil {
		return ""
	}
	if turnMetadata := strings.TrimSpace(headers.Get("X-Codex-Turn-Metadata")); turnMetadata != "" {
		if key := codexReasoningReplaySessionKeyFromTurnMetadata(turnMetadata); key != "" {
			return key
		}
	}
	if windowID := strings.TrimSpace(headerValueCaseInsensitive(headers, "X-Codex-Window-Id")); windowID != "" {
		return "window:" + windowID
	}
	for _, headerName := range []string{"Session_id", "session_id", "Session-Id"} {
		if value := strings.TrimSpace(headerValueCaseInsensitive(headers, headerName)); value != "" {
			return "session-id:" + value
		}
	}
	if conversationID := strings.TrimSpace(headerValueCaseInsensitive(headers, "Conversation_id")); conversationID != "" {
		return "conversation_id:" + conversationID
	}
	return ""
}

func codexReasoningReplaySessionKeyFromTurnMetadata(turnMetadata string) string {
	if promptCacheKey := strings.TrimSpace(gjson.Get(turnMetadata, "prompt_cache_key").String()); promptCacheKey != "" {
		return "prompt-cache:" + promptCacheKey
	}
	if windowID := strings.TrimSpace(gjson.Get(turnMetadata, "window_id").String()); windowID != "" {
		return "window:" + windowID
	}
	return ""
}

func headerValueCaseInsensitive(headers http.Header, key string) string {
	key = strings.TrimSpace(key)
	if headers == nil || key == "" {
		return ""
	}
	if val := strings.TrimSpace(headers.Get(key)); val != "" {
		return val
	}
	for existingKey, values := range headers {
		if !strings.EqualFold(existingKey, key) {
			continue
		}
		for _, value := range values {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
