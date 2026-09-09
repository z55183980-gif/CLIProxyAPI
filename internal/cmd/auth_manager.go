package cmd

import (
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
)

// newAuthManager creates a new authentication manager instance with a
// file-based token store. Claude is the only remaining OAuth provider.
//
// Returns:
//   - *sdkAuth.Manager: A configured authentication manager instance
func newAuthManager() *sdkAuth.Manager {
	store := sdkAuth.GetTokenStore()
	manager := sdkAuth.NewManager(store,
		sdkAuth.NewClaudeAuthenticator(),
	)
	return manager
}
