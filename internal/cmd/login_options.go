package cmd

// LoginOptions carries the shared interactive-login knobs used by the OAuth
// login flows in this package.
//
// This type was originally declared in openai_login.go alongside the Codex
// login flow. That file was removed with the Codex upstream, but the type
// itself is provider-neutral and the surviving Claude login flow depends on it,
// so it lives here now. The declaration is unchanged.
type LoginOptions struct {
	// NoBrowser indicates whether to skip opening the browser automatically.
	NoBrowser bool

	// CallbackPort overrides the local OAuth callback port when set (>0).
	CallbackPort int

	// Prompt allows the caller to provide interactive input when needed.
	Prompt func(prompt string) (string, error)
}
