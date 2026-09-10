package antigravity

import (
	"net/url"
	"testing"
)

func TestOAuthClientEnvironmentLoadedAtRequestTime(t *testing.T) {
	auth := &AntigravityAuth{}
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "first-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "test-secret")
	parsed, err := url.Parse(auth.BuildAuthURL("state", "http://localhost/callback"))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("client_id") != "first-client" {
		t.Fatal("authorization URL did not use configured client")
	}
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "updated-client")
	parsed, err = url.Parse(auth.BuildAuthURL("state", "http://localhost/callback"))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("client_id") != "updated-client" || ClientSecret() != "test-secret" {
		t.Fatal("credentials were read before runtime environment was loaded")
	}
}
