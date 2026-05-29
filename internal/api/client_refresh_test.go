package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/upuai-cloud/cli/internal/config"
)

// Regressão #5 (recursão infinita em 401 persistente) + #4 (refresh token no
// body, não na query). Servidor que sempre devolve 401 no protegido e um
// /auth/refresh que sempre devolve 200 com um token que segue ruim: sob o bug
// antigo o doRequest recursaria pra sempre. Com a guarda `retried`, deve bater
// no protegido exatamente 2x (original + 1 retry) e no refresh 1x.
func TestDoRequestRefreshRetryBoundedAndBodyToken(t *testing.T) {
	var protectedHits, refreshHits int32
	var refreshHadQuery bool
	var refreshBodyToken string

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshHits, 1)
		if r.URL.RawQuery != "" {
			refreshHadQuery = true
		}
		var b struct {
			RefreshToken string `json:"refreshToken"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &b)
		refreshBodyToken = b.RefreshToken
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"new-still-bad","refreshToken":"r2"}`))
	})
	mux.HandleFunc("/protected", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&protectedHits, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("HOME", t.TempDir())
	store := config.NewCredentialStore()
	if err := store.Save(&config.Credentials{Token: "old", RefreshToken: "r1", ApiURL: srv.URL}); err != nil {
		t.Fatalf("save creds: %v", err)
	}

	c := &Client{baseURL: srv.URL, httpClient: srv.Client(), credStore: store}

	if err := c.Get("/protected", nil); err == nil {
		t.Fatal("esperado erro do 401 persistente")
	}

	if got := atomic.LoadInt32(&protectedHits); got != 2 {
		t.Fatalf("esperado 2 hits no protegido (1 + 1 retry), veio %d — possível recursão infinita", got)
	}
	if got := atomic.LoadInt32(&refreshHits); got != 1 {
		t.Fatalf("esperado 1 refresh, veio %d", got)
	}
	if refreshHadQuery {
		t.Fatal("refresh token vazou na query string da URL")
	}
	if refreshBodyToken != "r1" {
		t.Fatalf("esperado refresh token no body = r1, veio %q", refreshBodyToken)
	}
}
