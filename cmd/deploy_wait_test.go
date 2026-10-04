package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

// newWaitAPI serve GET /deployments/dep-1 devolvendo "deploying" nas primeiras
// `pending` consultas e `final` dali em diante. polls conta as consultas.
func newWaitAPI(t *testing.T, pending int32, final string) (*api.Client, *atomic.Int32) {
	t.Helper()
	var polls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/deployments/dep-1", func(w http.ResponseWriter, r *http.Request) {
		status := final
		if polls.Add(1) <= pending {
			status = "deploying"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"dep-1","status":%q}`, status)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPUAI_API_URL", srv.URL)
	t.Setenv(config.EnvTokenVar, "upua_ci_token")

	prev := deployPollInterval
	deployPollInterval = time.Millisecond
	t.Cleanup(func() { deployPollInterval = prev })
	return api.NewClient(), &polls
}

// O --wait saía com "timed out" depois de 300s — e 52% dos deploys bem-sucedidos
// da plataforma levavam mais que isso (medido em 2026-10). Sem --wait-timeout o
// CLI não tem teto: espera o status terminal, por mais consultas que leve.
func TestWaitForDeployment_NoClientTimeoutByDefault(t *testing.T) {
	client, polls := newWaitAPI(t, 400, "success")

	dep, err := waitForDeploymentWithin(client, "dep-1", ui.FormatJSON, 0)
	if err != nil {
		t.Fatalf("esperar sem teto não pode falhar: %v", err)
	}
	if dep.Status != "success" {
		t.Fatalf("status = %q, want success", dep.Status)
	}
	if got := polls.Load(); got != 401 {
		t.Fatalf("consultas = %d, want 401 (400 pendentes + a terminal)", got)
	}
}

// Com teto explícito o comando para de esperar — e diz que o deploy continua.
func TestWaitForDeployment_ExplicitTimeoutStopsWaiting(t *testing.T) {
	client, _ := newWaitAPI(t, 1_000_000, "success")

	dep, err := waitForDeploymentWithin(client, "dep-1", ui.FormatJSON, 30*time.Millisecond)
	if err == nil {
		t.Fatal("esperava erro de --wait-timeout")
	}
	if dep == nil || dep.Status != "deploying" {
		t.Fatalf("deve devolver o último estado visto, got %+v", dep)
	}
	for _, want := range []string{"--wait-timeout", "still deploying", "keeps running"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("mensagem sem %q: %v", want, err)
		}
	}
}

// Falha terminal encerra a espera na hora, com ou sem teto.
func TestWaitForDeployment_ReturnsOnAnyTerminalStatus(t *testing.T) {
	for _, final := range []string{"failed", "build_failed", "cancelled", "superseded"} {
		t.Run(final, func(t *testing.T) {
			client, polls := newWaitAPI(t, 3, final)
			dep, err := waitForDeploymentWithin(client, "dep-1", ui.FormatJSON, 0)
			if err != nil || dep.Status != final {
				t.Fatalf("got %+v, %v; want status %s", dep, err, final)
			}
			if got := polls.Load(); got != 4 {
				t.Fatalf("consultas = %d, want 4", got)
			}
		})
	}
}

// O default dos dois comandos que esperam um deploy é "sem teto".
func TestWaitTimeoutFlagDefaultsToNoLimit(t *testing.T) {
	for name, flag := range map[string]string{
		"deploy": deployCmd.Flags().Lookup("wait-timeout").DefValue,
		"up":     upCmd.Flags().Lookup("wait-timeout").DefValue,
	} {
		if flag != "0" {
			t.Errorf("%s --wait-timeout default = %s, want 0 (sem teto)", name, flag)
		}
	}
}
