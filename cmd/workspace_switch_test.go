package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
)

// newSwitchServer devolve um servidor que responde /auth/switch-tenant com
// status/body dados, e captura o corpo recebido.
func newSwitchServer(t *testing.T, status int, body string, captured *map[string]string, hits *int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/switch-tenant", func(w http.ResponseWriter, r *http.Request) {
		*hits++
		raw, _ := io.ReadAll(r.Body)
		var parsed map[string]string
		_ = json.Unmarshal(raw, &parsed)
		*captured = parsed
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func seedSession(t *testing.T, apiURL string) *config.CredentialStore {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPUAI_API_URL", apiURL)
	t.Setenv(config.EnvTokenVar, "")

	store := config.NewCredentialStore()
	err := store.Save(&config.Credentials{
		Token:        "tokA",
		RefreshToken: "refA",
		ApiURL:       apiURL,
	})
	if err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	return store
}

func TestSwitchWorkspacePersistsNewSession(t *testing.T) {
	var captured map[string]string
	hits := 0
	srv := newSwitchServer(t, http.StatusOK,
		`{"userId":"u1","userName":"Gabriel","login":"g@x.com","token":"tokB","refreshToken":"refB","tenantId":"ws-tai","tenantName":"TAI"}`,
		&captured, &hits)

	store := seedSession(t, srv.URL)

	if err := switchWorkspace(api.NewClient(), "ws-tai"); err != nil {
		t.Fatalf("switchWorkspace: %v", err)
	}

	// O refresh atual vai no body: é o que a API revoga para não deixar a sessão
	// anterior viva até expirar.
	if captured["tenantId"] != "ws-tai" || captured["refreshToken"] != "refA" {
		t.Fatalf("body inesperado: %#v", captured)
	}

	creds, err := store.Load()
	if err != nil || creds == nil {
		t.Fatalf("load credentials: %v", err)
	}
	if creds.Token != "tokB" || creds.RefreshToken != "refB" {
		t.Fatalf("sessão não persistida: %+v", creds)
	}
}

// A API revoga o refresh anterior ANTES de responder. Se o novo par não for
// gravado, a máquina fica com um refresh morto e o usuário é deslogado no
// próximo 401. Um response sem token é o pior caso: parecia sucesso.
func TestSwitchWorkspaceRejectsEmptySession(t *testing.T) {
	var captured map[string]string
	hits := 0
	srv := newSwitchServer(t, http.StatusOK, `{"userId":"u1"}`, &captured, &hits)
	store := seedSession(t, srv.URL)

	err := switchWorkspace(api.NewClient(), "ws-tai")
	if err == nil {
		t.Fatal("esperado erro para sessão vazia")
	}

	creds, _ := store.Load()
	if creds.Token != "tokA" || creds.RefreshToken != "refA" {
		t.Fatalf("credenciais não devem ser sobrescritas por uma sessão vazia: %+v", creds)
	}
}

// Erro deve virar orientação acionável e vir do CÓDIGO estável do catálogo, não
// da mensagem (texto de UI, traduzível). E a sessão local fica intacta.
func TestSwitchWorkspaceExplainsNotAMember(t *testing.T) {
	var captured map[string]string
	hits := 0
	srv := newSwitchServer(t, http.StatusForbidden,
		`{"statusCode":403,"error":"ForbiddenError","code":"NOT_A_MEMBER","message":"You are not a member of this workspace"}`,
		&captured, &hits)
	store := seedSession(t, srv.URL)

	err := switchWorkspace(api.NewClient(), "ws-alheio")
	if err == nil {
		t.Fatal("esperado erro")
	}
	if !strings.Contains(err.Error(), "workspace list") {
		t.Fatalf("erro deveria orientar o usuário, veio: %v", err)
	}

	creds, _ := store.Load()
	if creds.Token != "tokA" {
		t.Fatalf("sessão corrompida após falha: %+v", creds)
	}
}

func TestSwitchWorkspaceExplainsImpersonation(t *testing.T) {
	var captured map[string]string
	hits := 0
	srv := newSwitchServer(t, http.StatusBadRequest,
		`{"statusCode":400,"code":"IMPERSONATION_ACTIVE","message":"Exit impersonation before switching workspace"}`,
		&captured, &hits)
	seedSession(t, srv.URL)

	err := switchWorkspace(api.NewClient(), "ws-tai")
	if err == nil || !strings.Contains(err.Error(), "impersonation") {
		t.Fatalf("esperado erro de impersonation acionável, veio: %v", err)
	}
}

// Machine token (UPUAI_TOKEN) é escopado a um workspace na criação; a API exige
// principal humano para trocar. O guard tem que barrar ANTES da chamada — senão
// o usuário leva um 403 genérico e tenta `upuai login`, que não resolve nada.
func TestSwitchWorkspaceBlockedForMachineToken(t *testing.T) {
	var captured map[string]string
	hits := 0
	srv := newSwitchServer(t, http.StatusOK, `{"token":"nope"}`, &captured, &hits)
	seedSession(t, srv.URL)
	t.Setenv(config.EnvTokenVar, "upua_ci_token")

	err := switchWorkspace(api.NewClient(), "ws-tai")
	if err == nil {
		t.Fatal("esperado erro com machine token")
	}
	if !strings.Contains(err.Error(), "token create") {
		t.Fatalf("erro deveria apontar a saída (emitir token no workspace destino), veio: %v", err)
	}
	if hits != 0 {
		t.Fatalf("nenhuma request deveria sair, houve %d", hits)
	}
}

// Listar workspaces é ler as memberships de uma pessoa; a API recusa token de
// máquina. O guard orienta antes da chamada, sem request saindo.
func TestListWorkspacesBlockedForMachineToken(t *testing.T) {
	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/tenant", func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	seedSession(t, srv.URL)
	t.Setenv(config.EnvTokenVar, "upua_ci_token")

	err := workspaceListCmd.RunE(workspaceListCmd, nil)
	if err == nil {
		t.Fatal("esperado erro com machine token")
	}
	if !strings.Contains(err.Error(), "unset "+config.EnvTokenVar) {
		t.Fatalf("erro deveria mandar tirar o token do ambiente (ele tem precedência sobre o login), veio: %v", err)
	}
	if hits != 0 {
		t.Fatalf("nenhuma request deveria sair, houve %d", hits)
	}
}
