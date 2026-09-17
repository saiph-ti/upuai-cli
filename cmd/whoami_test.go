package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

func newSelfTokenAPI(t *testing.T, status int, body string) *api.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/tokens/self", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer upua_ci_token" {
			t.Errorf("Authorization = %q, quero o token do ambiente", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// Sem credentials.json: CI e servidores só têm o token.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPUAI_API_URL", srv.URL)
	t.Setenv(config.EnvTokenVar, "upua_ci_token")
	t.Chdir(t.TempDir())
	return api.NewClient()
}

const selfTokenBody = `{"id":"tok-1","name":"ci","prefix":"upua_ab12","scopes":["DEPLOY"],"expiresAt":null,
"workspace":{"id":"ws-a","name":"Acme","slug":"acme"},"project":{"id":"proj-a","name":"landing-pages"}}`

const workspaceWideTokenBody = `{"id":"tok-2","name":"ci-all","prefix":"upua_cd34","scopes":["READ"],"expiresAt":"2026-12-31T00:00:00.000Z",
"workspace":{"id":"ws-a","name":"Acme","slug":"acme"},"project":null}`

func TestWhoamiWorkspaceWideToken(t *testing.T) {
	client := newSelfTokenAPI(t, http.StatusOK, workspaceWideTokenBody)
	self, err := client.GetSelfToken()
	if err != nil {
		t.Fatal(err)
	}
	if self.Project != nil || self.Workspace.Slug != "acme" || self.ExpiresAt == nil {
		t.Fatalf("decodificação de /tokens/self: %+v", self)
	}
	for _, format := range []ui.OutputFormat{ui.FormatJSON, ui.FormatTable} {
		if err := whoamiMachineToken(client, format); err != nil {
			t.Fatalf("formato %v: %v", format, err)
		}
	}
}

func TestWhoamiMachineTokenWithoutStoredLogin(t *testing.T) {
	client := newSelfTokenAPI(t, http.StatusOK, selfTokenBody)
	for _, format := range []ui.OutputFormat{ui.FormatJSON, ui.FormatTable} {
		if err := whoamiMachineToken(client, format); err != nil {
			t.Fatalf("formato %v: %v", format, err)
		}
	}
	if err := currentMachineTokenWorkspace(client, ui.FormatJSON); err != nil {
		t.Fatalf("workspace current: %v", err)
	}
}

// Token revogado tem de derrubar o pipeline com a causa, não virar "offline".
func TestWhoamiMachineTokenRejected(t *testing.T) {
	client := newSelfTokenAPI(t, http.StatusUnauthorized, `{"statusCode":401,"message":"Invalid or expired token"}`)
	err := whoamiMachineToken(client, ui.FormatTable)
	if err == nil || !strings.Contains(err.Error(), "revoked, expired or mistyped") {
		t.Fatalf("esperava recusa explícita, veio %v", err)
	}
	if err := currentMachineTokenWorkspace(client, ui.FormatTable); err == nil {
		t.Fatal("workspace current deveria falhar com token recusado")
	}
}
