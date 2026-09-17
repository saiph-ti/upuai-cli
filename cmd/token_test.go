package cmd

import (
	"testing"
	"time"

	"github.com/upuai-cloud/cli/internal/api"
)

// A API recusa token revogado ou expirado; a listagem não pode chamá-lo de ativo.
func TestTokenStatusMirrorsAPIRefusal(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ptr := func(s string) *string { return &s }

	tests := []struct {
		name  string
		token api.ApiToken
		want  string
	}{
		{name: "no expiry is active", token: api.ApiToken{}, want: "active"},
		{name: "future expiry is active", token: api.ApiToken{ExpiresAt: ptr("2026-09-18T12:00:00.000Z")}, want: "active"},
		{name: "past expiry is expired", token: api.ApiToken{ExpiresAt: ptr("2026-09-16T12:00:00.000Z")}, want: "expired"},
		{name: "expiring exactly now is expired", token: api.ApiToken{ExpiresAt: ptr("2026-09-17T12:00:00Z")}, want: "expired"},
		{name: "revoked wins over expiry", token: api.ApiToken{RevokedAt: ptr("2026-09-01T00:00:00Z"), ExpiresAt: ptr("2027-01-01T00:00:00Z")}, want: "revoked"},
		{name: "unparseable expiry is shown, not called active", token: api.ApiToken{ExpiresAt: ptr("soon")}, want: "expires soon"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tokenStatus(tc.token, now); got != tc.want {
				t.Fatalf("tokenStatus = %q, want %q", got, tc.want)
			}
		})
	}
}
