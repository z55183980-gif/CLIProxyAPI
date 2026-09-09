// Package translator wires the built-in request/response translators into the
// SDK translator registry via package init side effects.
//
// The leaf package path reads <upstream>/<client-format>. Keep the client
// formats supported by Claude and the generic OpenAI-compatible executor.
package translator

import (
	// Claude upstream, Gemini-shaped client surface.
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/claude/gemini"
	// Claude upstream, Interactions-shaped client surface.
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/claude/interactions"
	// Claude upstream, OpenAI chat-completions client surface.
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/claude/openai/chat-completions"
	// Claude upstream, OpenAI responses client surface.
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/claude/openai/responses"
	// Generic OpenAI-compatible upstream, Claude-shaped client surface.
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/claude"
	// Generic OpenAI-compatible upstream, OpenAI client surfaces.
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/openai/chat-completions"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/openai/responses"
)
