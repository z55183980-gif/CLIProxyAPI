package auth

import (
	"context"
	"errors"
	"io"
	"strings"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
)

type sub2apiStreamReadError struct {
	error
	cleanEOF bool
}

func (e *sub2apiStreamReadError) Unwrap() error { return e.error }

type sub2apiFailedEventError struct{ cause *Error }

func (*sub2apiFailedEventError) IsPreOutputStreamFailure() bool { return true }
func (e *sub2apiFailedEventError) Error() string                { return e.cause.Error() }
func (e *sub2apiFailedEventError) Unwrap() error                { return e.cause }
func (e *sub2apiFailedEventError) StatusCode() int              { return e.cause.HTTPStatus }

func sub2apiIsStreamFailure(err error) bool {
	var source interface{ IsPreOutputStreamFailure() bool }
	return errors.As(err, &source) && source.IsPreOutputStreamFailure()
}

func sub2apiStreamFailureRetryable(body string) bool {
	if sub2apiContextError(body) {
		return false
	}
	if strings.EqualFold(sub2apiErrorField(body, "code"), "cyber_policy") {
		return false
	}
	if sub2apiAccessState(body) {
		return true
	}
	status := sub2apiFailedEventStatus(body)
	if status == 403 {
		return sub2apiStreamAuthFailure(body)
	}
	if status == 429 {
		return true
	}
	if sub2apiTransient(400, body) {
		return true
	}
	if gjson.Get(body, "type").String() == "error" {
		if status == 401 || status == 529 {
			return true
		}
		message := strings.ToLower(sub2apiErrorField(body, "message"))
		return strings.Contains(message, "temporary") || strings.Contains(message, "try again") || strings.Contains(message, "please retry")
	}
	var parts []string
	for _, name := range []string{"message", "code", "type"} {
		value := gjson.Get(body, "response.error."+name).String()
		if value == "" {
			value = gjson.Get(body, "error."+name).String()
		}
		parts = append(parts, value)
	}
	combined := strings.ToLower(strings.Join(parts, " "))
	for _, marker := range []string{"invalid_request", "content_policy", "policy", "safety", "high-risk cyber", "not allowed", "violat"} {
		if strings.Contains(combined, marker) {
			return false
		}
	}
	return true
}

func (m *Manager) sub2apiStream(ctx context.Context, executor ProviderExecutor, a *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	sub2apiState(ctx).model = req.Model
	var stream *cliproxyexecutor.StreamResult
	var buffered []cliproxyexecutor.StreamChunk
	err := m.sub2apiAttempt(ctx, a, func() error {
		sub2apiState(ctx).headers = nil
		var errStart error
		stream, errStart = m.streamOnceWithCapacity(ctx, executor, a, req, opts)
		if errStart != nil {
			return errStart
		}
		stream, errStart = validateStreamResult(stream, nil)
		if errStart != nil {
			return &sub2apiStreamReadError{error: errStart, cleanEOF: true}
		}
		sub2apiState(ctx).headers = stream.Headers
		buffered, errStart = readSub2APIBootstrap(ctx, a.Provider, stream.Chunks)
		if errStart != nil {
			discardStreamChunks(stream.Chunks)
		}
		return errStart
	}, true)
	if err != nil {
		if stream != nil {
			return nil, newStreamBootstrapError(err, stream.Headers)
		}
		return nil, err
	}
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		for _, chunk := range buffered {
			select {
			case out <- chunk:
			case <-ctx.Done():
				discardStreamChunks(stream.Chunks)
				return
			}
		}
		for chunk := range stream.Chunks {
			out <- chunk
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: stream.Headers, Chunks: out}, nil
}

// Codex preamble events are staged until meaningful output, matching
// openai_gateway_passthrough.go. Claude commits its first nonempty payload.
func readSub2APIBootstrap(ctx context.Context, provider string, ch <-chan cliproxyexecutor.StreamChunk) ([]cliproxyexecutor.StreamChunk, error) {
	var buffered []cliproxyexecutor.StreamChunk
	var pending string
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case chunk, ok := <-ch:
			if !ok {
				return nil, &sub2apiStreamReadError{error: io.ErrUnexpectedEOF, cleanEOF: true}
			}
			if chunk.Err != nil {
				if errors.Is(chunk.Err, context.Canceled) || errors.Is(chunk.Err, context.DeadlineExceeded) {
					return nil, chunk.Err
				}
				if statusCodeFromError(chunk.Err) != 0 {
					return nil, chunk.Err
				}
				return nil, &sub2apiStreamReadError{error: chunk.Err}
			}
			buffered = append(buffered, chunk)
			if len(chunk.Payload) == 0 {
				continue
			}
			if provider == "claude" {
				return buffered, nil
			}
			pending += strings.ReplaceAll(string(chunk.Payload), "\r\n", "\n")
			for {
				if pending == "" {
					break
				}
				end := strings.Index(pending, "\n\n")
				if end < 0 {
					data := strings.TrimSpace(strings.TrimPrefix(pending, "data:"))
					if gjson.Valid(data) {
						typ := gjson.Get(data, "type").String()
						if typ == "response.failed" || typ == "error" {
							return nil, &sub2apiFailedEventError{&Error{HTTPStatus: sub2apiFailedEventStatus(data), Message: data}}
						}
						if typ == "response.created" || typ == "response.in_progress" || !sub2apiAddedEventStartsOutput([]byte(data), typ) {
							pending = ""
							break
						}
						return buffered, nil
					}
					// Executors may emit JSON payloads rather than framed SSE.
					if gjson.Valid(pending) || !strings.HasPrefix(pending, "data:") && !strings.HasPrefix(pending, "event:") && !strings.HasPrefix(pending, ":") {
						return buffered, nil
					}
					break
				}
				event := pending[:end]
				pending = pending[end+2:]
				for _, line := range strings.Split(event, "\n") {
					if !strings.HasPrefix(line, "data:") {
						continue
					}
					data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
					if data == "" {
						continue
					}
					typ := gjson.Get(data, "type").String()
					if typ == "response.created" || typ == "response.in_progress" || !sub2apiAddedEventStartsOutput([]byte(data), typ) {
						continue
					}
					if typ == "response.failed" || typ == "error" {
						return nil, &sub2apiFailedEventError{&Error{HTTPStatus: sub2apiFailedEventStatus(data), Message: data}}
					}
					return buffered, nil
				}
			}
		}
	}
}

func sub2apiFailedEventStatus(body string) int {
	if sub2apiContextError(body) {
		return 400
	}
	code := gjson.Get(body, "response.error.code").String()
	if code == "" {
		code = gjson.Get(body, "error.code").String()
	}
	typ := gjson.Get(body, "response.error.type").String()
	if typ == "" {
		typ = gjson.Get(body, "error.type").String()
	}
	typ, code = strings.ToLower(typ), strings.ToLower(code)
	combined := typ + " " + code + " " + strings.ToLower(sub2apiErrorField(body, "message"))
	for _, path := range []string{"response.error.status_code", "error.status_code", "status_code"} {
		status := int(gjson.Get(body, path).Int())
		if status == 401 || status == 403 || status == 429 || status == 529 {
			return status
		}
	}
	switch {
	case strings.Contains(combined, "rate_limit"):
		return 429
	case strings.Contains(typ, "invalid_request"):
		return 400
	case strings.Contains(combined, "authentication") || strings.Contains(combined, "unauthorized") || strings.Contains(combined, "invalid_api_key"):
		return 401
	case strings.Contains(combined, "permission") || strings.Contains(combined, "forbidden") || strings.Contains(combined, "access denied"):
		return 403
	case sub2apiAccessState(body):
		return 403
	case sub2apiCapacityShed(body):
		return 503
	default:
		return 502
	}
}

func sub2apiStreamAuthFailure(body string) bool {
	for _, path := range []string{"response.error.status_code", "error.status_code", "status_code"} {
		if gjson.Get(body, path).Int() == 401 {
			return true
		}
	}
	for _, path := range []string{"response.error.type", "error.type", "type"} {
		switch strings.ToLower(strings.TrimSpace(gjson.Get(body, path).String())) {
		case "authentication_error", "authentication_failed", "unauthorized_error":
			return true
		}
	}
	for _, path := range []string{"response.error.code", "error.code", "code"} {
		switch strings.ToLower(strings.TrimSpace(gjson.Get(body, path).String())) {
		case "invalid_api_key", "api_key_disabled", "unauthorized", "authentication_error", "invalid_token", "access_token_invalid", "token_revoked", "token_invalidated", "invalid_credentials", "credential_invalid":
			return true
		}
	}
	return false
}

func sub2apiAddedEventStartsOutput(payload []byte, eventType string) bool {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return true
	}

	switch strings.TrimSpace(eventType) {
	case "response.output_item.added":
		item := gjson.GetBytes(payload, "item")
		if !item.Exists() || !item.IsObject() {
			return true
		}
		switch strings.TrimSpace(item.Get("type").String()) {
		case "reasoning":
			if item.Get("encrypted_content").String() != "" {
				return true
			}
			summary := item.Get("summary")
			if !summary.IsArray() {
				return false
			}
			for _, part := range summary.Array() {
				if strings.TrimSpace(part.Get("type").String()) != "summary_text" || part.Get("text").String() != "" {
					return true
				}
			}
			return false
		case "message":
			content := item.Get("content")
			if !content.IsArray() {
				return false
			}
			for _, part := range content.Array() {
				switch strings.TrimSpace(part.Get("type").String()) {
				case "output_text":
					if part.Get("text").String() != "" {
						return true
					}
				case "refusal":
					if part.Get("refusal").String() != "" {
						return true
					}
				default:
					return true
				}
			}
			return false
		case "function_call":
			return item.Get("arguments").String() != ""
		case "custom_tool_call":
			return item.Get("input").String() != ""
		case "compaction":
			return item.Get("encrypted_content").String() != ""
		default:
			return true
		}
	case "response.content_part.added":
		part := gjson.GetBytes(payload, "part")
		if !part.Exists() || !part.IsObject() {
			return true
		}
		switch strings.TrimSpace(part.Get("type").String()) {
		case "output_text":
			return part.Get("text").String() != ""
		case "refusal":
			return part.Get("refusal").String() != ""
		default:
			return true
		}
	case "response.reasoning_summary_part.added":
		part := gjson.GetBytes(payload, "part")
		if !part.Exists() || !part.IsObject() || strings.TrimSpace(part.Get("type").String()) != "summary_text" {
			return true
		}
		return part.Get("text").String() != ""
	default:
		return true
	}
}
