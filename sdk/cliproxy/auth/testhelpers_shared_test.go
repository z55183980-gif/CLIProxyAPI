package auth

// Provider-neutral test helpers.
//
// boolPointer was originally declared in selector_antigravity_subagent_test.go,
// which was removed with the Antigravity upstream. The helper itself is
// provider-neutral and selector_lcp_test.go depends on it, so it lives here now.

func boolPointer(b bool) *bool {
	return &b
}
