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

type publicAccessPut struct {
	Enabled      bool     `json:"enabled"`
	AllowedCidrs []string `json:"allowedCidrs"`
}

// newPublicAccessAPI serve o estado do endpoint público e registra cada PUT.
// `current` é o que o GET devolve — é o que `db public enable` sem flags deve
// preservar.
func newPublicAccessAPI(t *testing.T, current api.PublicAccessInfo, puts *[]publicAccessPut) *api.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/environments/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/database/public-access") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(current)
		case http.MethodPut:
			raw, _ := io.ReadAll(r.Body)
			var put publicAccessPut
			_ = json.Unmarshal(raw, &put)
			*puts = append(*puts, put)
			_ = json.NewEncoder(w).Encode(api.PublicAccessInfo{
				Enabled:      put.Enabled,
				Host:         "abc123xy.db.upuai.cloud",
				Port:         5432,
				AllowedCidrs: put.AllowedCidrs,
			})
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPUAI_API_URL", srv.URL)
	t.Setenv(config.EnvTokenVar, "upua_ci_token")
	return api.NewClient()
}

func TestNormalizeAllowCIDRs(t *testing.T) {
	got, err := normalizeAllowCIDRs([]string{" 203.0.113.7 ", "203.0.113.7/24", "203.0.113.0/24", "2001:db8::1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"203.0.113.7/32", "203.0.113.0/24", "2001:db8::1/128"}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v; want %v", got, want)
		}
	}

	for _, bad := range []string{"nope", "203.0.113.0/33", "10.0.0.1-10.0.0.9"} {
		if _, err := normalizeAllowCIDRs([]string{bad}); err == nil {
			t.Errorf("%q: quero erro de validação", bad)
		}
	}
}

func TestResolvePublicAccessAllowList(t *testing.T) {
	restore := func() {
		dbAllowCIDRs, dbAllowAny = nil, false
	}
	t.Cleanup(restore)

	t.Run("--allow substitui a lista", func(t *testing.T) {
		restore()
		dbAllowCIDRs = []string{"10.0.0.5"}
		got, err := resolvePublicAccessAllowList(&api.PublicAccessInfo{
			Enabled:      true,
			AllowedCidrs: []string{"203.0.113.0/24"},
		})
		if err != nil || len(got) != 1 || got[0] != "10.0.0.5/32" {
			t.Fatalf("got %v, %v", got, err)
		}
	})

	t.Run("sem flag preserva a allowlist atual", func(t *testing.T) {
		restore()
		got, err := resolvePublicAccessAllowList(&api.PublicAccessInfo{
			Enabled:      true,
			AllowedCidrs: []string{"203.0.113.0/24"},
		})
		if err != nil || len(got) != 1 || got[0] != "203.0.113.0/24" {
			t.Fatalf("got %v, %v; um banco restrito não pode abrir por omissão", got, err)
		}
	})

	t.Run("--any zera", func(t *testing.T) {
		restore()
		dbAllowAny = true
		got, err := resolvePublicAccessAllowList(&api.PublicAccessInfo{
			Enabled:      true,
			AllowedCidrs: []string{"203.0.113.0/24"},
		})
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v, %v", got, err)
		}
	})

	t.Run("--any com --allow é recusado", func(t *testing.T) {
		restore()
		dbAllowAny, dbAllowCIDRs = true, []string{"10.0.0.5"}
		if _, err := resolvePublicAccessAllowList(nil); err == nil {
			t.Fatal("quero erro de flags mutuamente exclusivas")
		}
	})

	t.Run("endpoint desligado não herda nada", func(t *testing.T) {
		restore()
		got, err := resolvePublicAccessAllowList(&api.PublicAccessInfo{
			Enabled:      false,
			AllowedCidrs: []string{"203.0.113.0/24"},
		})
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v, %v", got, err)
		}
	})
}

func TestSetDatabasePublicAccessWire(t *testing.T) {
	var puts []publicAccessPut
	client := newPublicAccessAPI(t, api.PublicAccessInfo{Enabled: false}, &puts)

	if _, err := client.SetDatabasePublicAccess("env-a", "svc-db", true, []string{"203.0.113.7/32"}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// nil precisa virar lista vazia no wire: a API distingue "abrir para
	// qualquer IP" de campo ausente pelo valor.
	if _, err := client.SetDatabasePublicAccess("env-a", "svc-db", false, nil); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if len(puts) != 2 {
		t.Fatalf("PUTs = %+v", puts)
	}
	if !puts[0].Enabled || len(puts[0].AllowedCidrs) != 1 || puts[0].AllowedCidrs[0] != "203.0.113.7/32" {
		t.Fatalf("PUT[0] = %+v", puts[0])
	}
	if puts[1].Enabled || puts[1].AllowedCidrs == nil || len(puts[1].AllowedCidrs) != 0 {
		t.Fatalf("PUT[1] = %+v; quero enabled=false e allowedCidrs=[]", puts[1])
	}
}
