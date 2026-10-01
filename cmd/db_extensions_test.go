package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
)

const dbOpsBase = "/environments/env-a/services/svc-db"

type dbOpsRoute struct {
	status int
	body   string
}

type dbOpsRequest struct {
	Method string
	Path   string
	Body   string
}

// newDBOpsAPI sobe uma API fake com o projeto proj-a (ambiente env-a e o banco
// svc-db linkado) e responde as rotas de `routes` ("MÉTODO /caminho"). Toda
// chamada fora de ambientes/serviços é registrada — os testes travam o wire
// (método, caminho, corpo) e a ausência de mutação quando o comando recusa.
func newDBOpsAPI(t *testing.T, routes map[string]dbOpsRoute) *[]dbOpsRequest {
	t.Helper()
	var requests []dbOpsRequest
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/proj-a/environments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"env-a","name":"production"}]`))
	})
	mux.HandleFunc("/projects/proj-a/services", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"svc-db","name":"db","type":"database","slug":"db"},
			{"id":"svc-web","name":"web","type":"github","slug":"web"}
		]`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		requests = append(requests, dbOpsRequest{Method: r.Method, Path: r.URL.EscapedPath(), Body: string(raw)})
		route, ok := routes[r.Method+" "+r.URL.EscapedPath()]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(route.status)
		_, _ = w.Write([]byte(route.body))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPUAI_API_URL", srv.URL)
	t.Setenv(config.EnvTokenVar, "upua_ci_token")

	cfg := linkedConfig()
	cfg.ServiceID = "svc-db"
	linkDirectory(t, cfg)
	workspacePinChecked = true
	useDevNullStdin(t)

	prevOut, prevYes, prevSvc := flagOutput, flagYes, dbServiceRef
	prevWait, prevTimeout := dbUpdateWait, dbUpdateWaitTimeout
	t.Cleanup(func() {
		flagOutput, flagYes, dbServiceRef = prevOut, prevYes, prevSvc
		dbUpdateWait, dbUpdateWaitTimeout = prevWait, prevTimeout
	})
	flagOutput, flagYes, dbServiceRef = "", false, ""
	dbUpdateWait, dbUpdateWaitTimeout = false, dbUpdateDefaultWaitTimeout
	return &requests
}

// captureStdout devolve o que foi escrito no stdout enquanto fn rodava.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	defer func() { os.Stdout = prev }()
	fn()
	_ = w.Close()
	return <-done
}

const extensionListJSON = `{"extensions":[
	{"name":"postgis","category":"geo","description":"PostGIS geometry and geography spatial types and functions","available":true,"installed":true,"installedVersion":"3.6.3","defaultVersion":"3.6.4","updateAvailable":true},
	{"name":"vector","category":"search","description":"vector data type and ivfflat and hnsw access methods","available":true,"installed":false,"installedVersion":"","defaultVersion":"0.8.1","updateAvailable":false},
	{"name":"pgrouting","category":"geo","description":"pgRouting Extension","available":false,"installed":false,"installedVersion":"","defaultVersion":"","updateAvailable":false}
]}`

const extensionEnabledJSON = `{"extensions":[
	{"name":"postgis","category":"geo","description":"PostGIS","available":true,"installed":true,"installedVersion":"3.6.4","defaultVersion":"3.6.4","updateAvailable":false},
	{"name":"postgis_topology","category":"geo","description":"PostGIS topology","available":true,"installed":true,"installedVersion":"3.6.4","defaultVersion":"3.6.4","updateAvailable":false}
]}`

func TestDBExtensionsListTable(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"GET " + dbOpsBase + "/database/extensions": {200, extensionListJSON},
	})

	var err error
	out := captureStdout(t, func() { err = dbExtensionsCmd.RunE(dbExtensionsCmd, nil) })
	if err != nil {
		t.Fatalf("db extensions: %v", err)
	}
	for _, want := range []string{
		"postgis", "enabled", "3.6.3 → 3.6.4",
		"vector", "available", "0.8.1",
		"pgrouting", "unavailable",
		"upuai db update",
		"upuai db extensions update <name>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
	if len(*requests) != 1 || (*requests)[0].Method != http.MethodGet {
		t.Fatalf("requests = %+v, quero só o GET", *requests)
	}
}

// -o json devolve o payload da API como veio — é contrato para scripts.
func TestDBExtensionsListJSON(t *testing.T) {
	newDBOpsAPI(t, map[string]dbOpsRoute{
		"GET " + dbOpsBase + "/database/extensions": {200, extensionListJSON},
	})
	flagOutput = "json"

	var err error
	out := captureStdout(t, func() { err = dbExtensionsCmd.RunE(dbExtensionsCmd, nil) })
	if err != nil {
		t.Fatalf("db extensions -o json: %v", err)
	}
	var got map[string][]map[string]any
	if jerr := json.Unmarshal([]byte(out), &got); jerr != nil {
		t.Fatalf("saída não é JSON: %v\n%s", jerr, out)
	}
	exts := got["extensions"]
	if len(exts) != 3 || exts[0]["name"] != "postgis" || exts[0]["installedVersion"] != "3.6.3" || exts[0]["updateAvailable"] != true {
		t.Fatalf("JSON = %+v", got)
	}
}

func TestDBExtensionsEnableWire(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"PUT " + dbOpsBase + "/database/extensions/postgis": {200, extensionEnabledJSON},
	})

	var err error
	// Capitalização do usuário não pode virar 400: a allowlist é minúscula.
	out := captureStdout(t, func() { err = dbExtensionsEnableCmd.RunE(dbExtensionsEnableCmd, []string{" PostGIS "}) })
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !strings.Contains(out, "postgis 3.6.4 enabled") {
		t.Fatalf("saída = %q", out)
	}
	want := dbOpsRequest{Method: http.MethodPut, Path: dbOpsBase + "/database/extensions/postgis", Body: ""}
	if len(*requests) != 1 || (*requests)[0] != want {
		t.Fatalf("requests = %+v, quero %+v", *requests, want)
	}
}

// Nomes com caractere fora do unreserved (ex: espaço) seguem escapados no path —
// nunca mudam a rota chamada.
func TestDBExtensionsEnableEscapesName(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{})

	_ = captureStdout(t, func() { _ = dbExtensionsEnableCmd.RunE(dbExtensionsEnableCmd, []string{"a b/c"}) })
	if len(*requests) != 1 || (*requests)[0].Path != dbOpsBase+"/database/extensions/a%20b%2Fc" {
		t.Fatalf("requests = %+v", *requests)
	}
}

func TestDBExtensionsDisableNeedsConfirmationWithoutTTY(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"DELETE " + dbOpsBase + "/database/extensions/postgis": {200, extensionListJSON},
	})

	err := dbExtensionsDisableCmd.RunE(dbExtensionsDisableCmd, []string{"postgis"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("erro = %v, quero pedido de --yes (stdin não é TTY nos testes)", err)
	}
	if len(*requests) != 0 {
		t.Fatalf("nenhuma chamada deveria sair sem confirmação, houve %+v", *requests)
	}
}

func TestDBExtensionsDisableWithYes(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"DELETE " + dbOpsBase + "/database/extensions/vector": {200, extensionListJSON},
	})
	flagYes = true

	var err error
	out := captureStdout(t, func() { err = dbExtensionsDisableCmd.RunE(dbExtensionsDisableCmd, []string{"vector"}) })
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if !strings.Contains(out, "vector disabled") {
		t.Fatalf("saída = %q", out)
	}
	if len(*requests) != 1 || (*requests)[0].Method != http.MethodDelete {
		t.Fatalf("requests = %+v", *requests)
	}
}

func TestDBExtensionsUpdateWire(t *testing.T) {
	requests := newDBOpsAPI(t, map[string]dbOpsRoute{
		"POST " + dbOpsBase + "/database/extensions/postgis/update": {200, extensionEnabledJSON},
	})

	var err error
	out := captureStdout(t, func() { err = dbExtensionsUpdateCmd.RunE(dbExtensionsUpdateCmd, []string{"postgis"}) })
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(out, "postgis is at 3.6.4") {
		t.Fatalf("saída = %q", out)
	}
	if len(*requests) != 1 || (*requests)[0].Method != http.MethodPost {
		t.Fatalf("requests = %+v", *requests)
	}
}

// O erro do disable bloqueado chega da API (409 + details.dependents) e precisa
// mostrar o que depende da extensão — sem isso o usuário não sabe o que remover.
func TestDBExtensionsDisableInUseShowsDependents(t *testing.T) {
	newDBOpsAPI(t, map[string]dbOpsRoute{
		"DELETE " + dbOpsBase + "/database/extensions/postgis": {409, `{
			"statusCode":409,"error":"DB_EXTENSION_IN_USE","message":"extension in use",
			"details":{"extension":"postgis","dependents":"column geom of table places depends on type geometry","actionable":true,"retryable":false},
			"requestId":"req-1"}`},
	})
	flagYes = true

	err := dbExtensionsDisableCmd.RunE(dbExtensionsDisableCmd, []string{"postgis"})
	if err == nil {
		t.Fatal("quero erro")
	}
	for _, want := range []string{"cannot disable postgis", "column geom of table places depends on type geometry", "CASCADE", "requestId: req-1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("erro sem %q: %v", want, err)
		}
	}
	// A mensagem amigável não pode esconder o código estável de quem ramifica.
	if api.ErrorCode(err) != "DB_EXTENSION_IN_USE" || api.StatusCode(err) != http.StatusConflict {
		t.Fatalf("code/status perdidos: %q %d", api.ErrorCode(err), api.StatusCode(err))
	}
}

func TestExplainDatabaseError(t *testing.T) {
	cases := []struct {
		name    string
		apiErr  *api.APIError
		ext     string
		want    []string
		notWant []string
	}{
		{
			name:   "imagem antiga pede a atualização",
			apiErr: &api.APIError{StatusCode: 409, Code: "DB_EXTENSION_UNAVAILABLE", Details: map[string]string{"extension": "postgis", "updateAvailable": "true"}},
			ext:    "postgis",
			want:   []string{"postgis", "upuai db update"},
		},
		{
			name:    "indisponível sem atualização não manda atualizar",
			apiErr:  &api.APIError{StatusCode: 409, Code: "DB_EXTENSION_UNAVAILABLE", Details: map[string]string{"extension": "postgis", "updateAvailable": "false"}},
			ext:     "postgis",
			want:    []string{"not available on this database"},
			notWant: []string{"upuai db update"},
		},
		{
			name:   "fora da allowlist",
			apiErr: &api.APIError{StatusCode: 400, Code: "DB_EXTENSION_NOT_ALLOWED", Details: map[string]string{"extension": "pg_cron"}},
			ext:    "pg_cron",
			want:   []string{"pg_cron is not a platform-managed extension", "upuai db extensions"},
		},
		{
			name:   "operação em andamento",
			apiErr: &api.APIError{StatusCode: 409, Code: "DB_OPERATION_IN_PROGRESS"},
			want:   []string{"another operation", "retry"},
		},
		{
			name:   "lock",
			apiErr: &api.APIError{StatusCode: 409, Code: "DB_OBJECT_LOCKED"},
			want:   []string{"lock", "retry"},
		},
		{
			name:   "read-only",
			apiErr: &api.APIError{StatusCode: 409, Code: "DB_READ_ONLY"},
			want:   []string{"read-only"},
		},
		{
			name:   "risco de collation orienta suporte",
			apiErr: &api.APIError{StatusCode: 409, Code: "DB_MAINTENANCE_COLLATION_RISK", Details: map[string]string{"databases": "app,reports"}},
			want:   []string{"collations", "app,reports", "contact support"},
		},
		{
			name:   "manutenção pendente",
			apiErr: &api.APIError{StatusCode: 409, Code: "DB_MAINTENANCE_REQUIRED"},
			want:   []string{"upuai db update"},
		},
		{
			name:   "imagem indisponível",
			apiErr: &api.APIError{StatusCode: 500, Code: "DB_IMAGE_UNAVAILABLE"},
			want:   []string{"not available on the platform"},
		},
		{
			name:   "deploy em andamento (reason)",
			apiErr: &api.APIError{StatusCode: 409, Code: "OPERATION_NOT_ALLOWED", Details: map[string]string{"reason": "deploymentInProgress"}},
			want:   []string{"deployment is in progress"},
		},
		{
			name:   "serviço que não é banco gerenciado (reason)",
			apiErr: &api.APIError{StatusCode: 409, Code: "OPERATION_NOT_ALLOWED", Details: map[string]string{"reason": "managedDatabaseOnly"}},
			want:   []string{"managed database services"},
		},
		{
			name:    "extensões num banco que não é Postgres (reason)",
			apiErr:  &api.APIError{StatusCode: 409, Code: "OPERATION_NOT_ALLOWED", Details: map[string]string{"reason": "postgresOnly"}},
			want:    []string{"only available on managed PostgreSQL"},
			notWant: []string{"API error 409"},
		},
		{
			name:   "código desconhecido preserva o erro original",
			apiErr: &api.APIError{StatusCode: 500, Message: "Internal server error", Code: "INTERNAL_ERROR", RequestID: "req-9"},
			want:   []string{"enable postgis", "API error 500", "req-9"},
			ext:    "postgis",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := explainDatabaseError(tc.apiErr, tc.ext, "enable postgis")
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("erro sem %q: %v", w, err)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(err.Error(), nw) {
					t.Errorf("erro não deveria conter %q: %v", nw, err)
				}
			}
			var apiErr *api.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != tc.apiErr.Code {
				t.Fatalf("causa perdida: %v", err)
			}
		})
	}
}

func TestExplainDatabaseErrorNonAPI(t *testing.T) {
	err := explainDatabaseError(errors.New("request failed: dial tcp: refused"), "postgis", "enable postgis")
	if !strings.Contains(err.Error(), "enable postgis: request failed") {
		t.Fatalf("erro = %v", err)
	}
}
