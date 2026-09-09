package auth

import (
	"testing"
	"time"
)

func TestProviderRefreshLeads(t *testing.T) {
	tests := []struct {
		name          string
		authenticator Authenticator
		want          time.Duration
	}{
		{name: "claude", authenticator: NewClaudeAuthenticator(), want: 4 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.authenticator.RefreshLead(); got == nil || *got != tt.want {
				t.Fatalf("RefreshLead() = %v, want %v", got, tt.want)
			}
		})
	}
}
