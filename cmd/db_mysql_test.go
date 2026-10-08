package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/cabundle"
	"github.com/upuai-cloud/cli/internal/config"
)

// ─── fixtures ────────────────────────────────────────────────────────────────

const testMySQLPassword = "s3cretPassw0rd"

func mysqlAccessJSON(enabled, tlsReady bool) string {
	if !enabled {
		return fmt.Sprintf(`{"engine":"mysql","enabled":false,"host":"","port":0,"connectionString":"","allowedCidrs":[],"username":"","password":"","database":"","serverTlsReady":%t}`, tlsReady)
	}
	return fmt.Sprintf(`{"engine":"mysql","enabled":true,"host":"orders.db.upuai.cloud","port":23307,`+
		`"connectionString":"mysql://app:%s@orders.db.upuai.cloud:23307/app?ssl-mode=VERIFY_IDENTITY",`+
		`"allowedCidrs":[],"username":"app","password":"%s","database":"app","serverTlsReady":%t}`, testMySQLPassword, testMySQLPassword, tlsReady)
}

// pgLegacyJSON: resposta de uma API anterior ao MySQL público (sem engine).
const pgLegacyJSON = `{"enabled":true,"host":"abc123xy.db.upuai.cloud","port":5432,"connectionString":"postgresql://app:pw@abc123xy.db.upuai.cloud:5432/app?sslmode=verify-full","allowedCidrs":["203.0.113.0/24"]}`

const pgDisabledLegacyJSON = `{"enabled":false,"host":"","port":0,"connectionString":""}`

// publicAccessHandler decide a resposta do n-ésimo (0-based) acesso ao
// endpoint public-access para o método dado.
type publicAccessHandler func(method string, n int, body string) (int, string)

// newPublicAccessFake sobe a API fake (projeto proj-a, ambiente env-a, banco
// svc-db linkado) com o endpoint public-access dinâmico. Devolve os requests
// feitos ao endpoint, em ordem.
func newPublicAccessFake(t *testing.T, h publicAccessHandler) *[]dbOpsRequest {
	t.Helper()
	var (
		mu       sync.Mutex
		requests []dbOpsRequest
		counts   = map[string]int{}
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/proj-a/environments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"env-a","name":"production"}]`))
	})
	mux.HandleFunc("/projects/proj-a/services", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"svc-db","name":"Orders DB","type":"database","slug":"orders-db"},{"id":"svc-web","name":"web","type":"github","slug":"web"}]`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != dbOpsBase+"/database/public-access" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, dbOpsRequest{Method: r.Method, Path: r.URL.Path, Body: string(raw)})
		n := counts[r.Method]
		counts[r.Method]++
		mu.Unlock()
		status, body := h(r.Method, n, string(raw))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
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
	resetDBFlags(t)
	return &requests
}

// resetDBFlags zera os globais dos comandos db e os restaura no fim do teste.
func resetDBFlags(t *testing.T) {
	t.Helper()
	prevOut, prevYes, prevSvc := flagOutput, flagYes, dbServiceRef
	prevBackup, prevRestore, prevAuto, prevPrint := dbBackupOut, dbRestoreIn, dbAutoEnable, dbConnectPrint
	prevAllow, prevAny := dbAllowCIDRs, dbAllowAny
	prevPoll, prevWait := mysqlTLSPollInterval, mysqlTLSWaitTimeout
	prevProbeT, prevProbeI, prevProbeW := mysqlRouteProbeTimeout, mysqlRouteProbeInterval, mysqlRouteProbeReadWindow
	prevProbe := mysqlRouteProbe
	t.Cleanup(func() {
		mysqlRouteProbe = prevProbe
		flagOutput, flagYes, dbServiceRef = prevOut, prevYes, prevSvc
		dbBackupOut, dbRestoreIn, dbAutoEnable, dbConnectPrint = prevBackup, prevRestore, prevAuto, prevPrint
		dbAllowCIDRs, dbAllowAny = prevAllow, prevAny
		mysqlTLSPollInterval, mysqlTLSWaitTimeout = prevPoll, prevWait
		mysqlRouteProbeTimeout, mysqlRouteProbeInterval, mysqlRouteProbeReadWindow = prevProbeT, prevProbeI, prevProbeW
	})
	flagOutput, flagYes, dbServiceRef = "", false, ""
	dbBackupOut, dbRestoreIn, dbAutoEnable, dbConnectPrint = "", "", false, false
	dbAllowCIDRs, dbAllowAny = nil, false
	mysqlTLSPollInterval, mysqlTLSWaitTimeout = time.Millisecond, 200*time.Millisecond
	mysqlRouteProbeTimeout, mysqlRouteProbeInterval, mysqlRouteProbeReadWindow = time.Millisecond, time.Millisecond, 50*time.Millisecond
	// Nunca disca o host da fixture (*.db.upuai.cloud resolve para o cluster).
	probedAddrs = nil
	mysqlRouteProbe = func(addr string, timeout, interval time.Duration) bool {
		probedAddrs = append(probedAddrs, addr)
		return true
	}
}

// probedAddrs registra os endereços que a sonda da rota recebeu no teste.
var probedAddrs []string

func countMethod(reqs []dbOpsRequest, method string) int {
	n := 0
	for _, r := range reqs {
		if r.Method == method {
			n++
		}
	}
	return n
}

// ─── sabor do cliente e argv ─────────────────────────────────────────────────

func TestDetectMySQLFlavor(t *testing.T) {
	cases := []struct {
		out  string
		want mysqlFlavor
	}{
		{"mysql  Ver 8.4.3 for macos15.0 on arm64 (Homebrew)", mysqlFlavorOracle},
		{"mysql  Ver 8.0.39-0ubuntu0.22.04.1 for Linux on x86_64 ((Ubuntu))", mysqlFlavorOracle},
		{"mysqldump  Ver 8.4.3 for macos15.0 on arm64 (Homebrew)", mysqlFlavorOracle},
		{"C:\\Program Files\\MySQL\\MySQL Server 8.4\\bin\\mysql.exe  Ver 8.4.2 for Win64 on x86_64 (MySQL Community Server - GPL)", mysqlFlavorOracle},
		{"mysql  Ver 15.1 Distrib 10.6.18-MariaDB, for debian-linux-gnu (x86_64) using  EditLine wrapper", mysqlFlavorMariaDB},
		{"mysql from 11.4.2-MariaDB, client 15.2 for debian-linux-gnu (x86_64) using  EditLine wrapper", mysqlFlavorMariaDB},
		{"mysqldump  Ver 10.19 Distrib 10.11.8-MariaDB, for debian-linux-gnu (x86_64)", mysqlFlavorMariaDB},
		{"", mysqlFlavorOracle},
	}
	for _, tc := range cases {
		if got := detectMySQLFlavor(tc.out); got != tc.want {
			t.Errorf("detectMySQLFlavor(%q) = %s, quero %s", tc.out, got, tc.want)
		}
	}
}

func TestFindMySQLTool(t *testing.T) {
	prevLook, prevVer := mysqlLookPath, mysqlToolVersion
	t.Cleanup(func() { mysqlLookPath, mysqlToolVersion = prevLook, prevVer })

	cases := []struct {
		name       string
		onPath     map[string]string // nome → saída de --version
		names      []string
		wantName   string
		wantFlavor mysqlFlavor
		wantErr    bool
	}{
		{"mysql da Oracle", map[string]string{"mysql": "mysql  Ver 8.4.3 for macos15.0 on arm64 (Homebrew)", "mariadb": "x"}, mysqlClientNames, "mysql", mysqlFlavorOracle, false},
		{"mysql do MariaDB (Debian/Ubuntu)", map[string]string{"mysql": "mysql  Ver 15.1 Distrib 10.6.18-MariaDB, for debian-linux-gnu"}, mysqlClientNames, "mysql", mysqlFlavorMariaDB, false},
		{"só o binário mariadb", map[string]string{"mariadb": "mariadb from 11.4.2-MariaDB, client 15.2"}, mysqlClientNames, "mariadb", mysqlFlavorMariaDB, false},
		{"mariadb-dump mesmo sem --version", map[string]string{"mariadb-dump": ""}, mysqlDumpNames, "mariadb-dump", mysqlFlavorMariaDB, false},
		{"mysqldump da Oracle", map[string]string{"mysqldump": "mysqldump  Ver 8.4.3 for Linux on x86_64 (MySQL Community Server - GPL)"}, mysqlDumpNames, "mysqldump", mysqlFlavorOracle, false},
		{"nenhum cliente", map[string]string{}, mysqlClientNames, "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mysqlLookPath = func(name string) (string, error) {
				if _, ok := tc.onPath[name]; ok {
					return "/fake/bin/" + name, nil
				}
				return "", exec.ErrNotFound
			}
			mysqlToolVersion = func(path string) (string, error) {
				out := tc.onPath[filepath.Base(path)]
				if out == "" {
					return "", errors.New("no version")
				}
				return out, nil
			}
			got, err := findMySQLTool(tc.names)
			if tc.wantErr {
				if err == nil {
					t.Fatal("quero erro de cliente ausente")
				}
				for _, hint := range []string{"mysql not found", "brew install mysql-client", "apt install default-mysql-client", "MySQL Installer", "upuai db connect --print"} {
					if !strings.Contains(err.Error(), hint) {
						t.Errorf("erro sem %q: %v", hint, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tc.wantName || got.Flavor != tc.wantFlavor || got.Path != "/fake/bin/"+tc.wantName {
				t.Fatalf("got %+v, quero %s/%s", got, tc.wantName, tc.wantFlavor)
			}
		})
	}
}

func TestMySQLArgs(t *testing.T) {
	target := mysqlTarget{Host: "orders.db.upuai.cloud", Port: 23307, User: "app", Database: "app"}
	const opt, ca = "/tmp/upuai-mysql-1/client.cnf", "/etc/ssl/cert.pem"
	base := []string{"--defaults-file=" + opt, "--host=orders.db.upuai.cloud", "--port=23307", "--user=app"}
	oracleTLS := []string{"--ssl-mode=VERIFY_IDENTITY", "--ssl-ca=" + ca}
	mariaTLS := []string{"--ssl", "--ssl-verify-server-cert", "--ssl-ca=" + ca}
	dumpFlags := []string{"--single-transaction", "--routines", "--events", "--triggers"}

	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"connect/restore Oracle", mysqlClientArgs(mysqlFlavorOracle, opt, ca, target),
			slices.Concat(base, oracleTLS, []string{"--database=app"})},
		{"connect/restore MariaDB", mysqlClientArgs(mysqlFlavorMariaDB, opt, ca, target),
			slices.Concat(base, mariaTLS, []string{"--database=app"})},
		{"backup Oracle", mysqlDumpArgs(mysqlFlavorOracle, opt, ca, target),
			slices.Concat(base, oracleTLS, dumpFlags, []string{"--set-gtid-purged=OFF", "--no-tablespaces", "app"})},
		{"backup MariaDB (sem --set-gtid-purged)", mysqlDumpArgs(mysqlFlavorMariaDB, opt, ca, target),
			slices.Concat(base, mariaTLS, dumpFlags, []string{"--no-tablespaces", "app"})},
		{"sem banco não manda --database", mysqlClientArgs(mysqlFlavorOracle, opt, ca, mysqlTarget{Host: "h", Port: 1, User: "u"}),
			[]string{"--defaults-file=" + opt, "--host=h", "--port=1", "--user=u", "--ssl-mode=VERIFY_IDENTITY", "--ssl-ca=" + ca}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !slices.Equal(tc.got, tc.want) {
				t.Fatalf("argv =\n  %q\nquero\n  %q", tc.got, tc.want)
			}
			// O cliente MySQL só aceita o arquivo de opções como PRIMEIRO argumento.
			if !strings.HasPrefix(tc.got[0], "--defaults-file=") {
				t.Fatalf("primeiro argumento = %q", tc.got[0])
			}
			for _, a := range tc.got {
				if strings.Contains(a, "password") {
					t.Fatalf("senha/flag de senha no argv: %q", a)
				}
			}
		})
	}
}

func TestMySQLTargetFromValidates(t *testing.T) {
	ok := &api.PublicAccessInfo{Engine: "mysql", Host: "h", Port: 23306, Username: "app", Password: "p", Database: "app"}
	if _, err := mysqlTargetFrom(ok); err != nil {
		t.Fatalf("válido: %v", err)
	}
	for name, info := range map[string]*api.PublicAccessInfo{
		"sem host":        {Port: 1, Username: "u", Password: "p"},
		"sem porta":       {Host: "h", Username: "u", Password: "p"},
		"sem usuário":     {Host: "h", Port: 1, Password: "p"},
		"sem senha":       {Host: "h", Port: 1, Username: "u"},
		"banco com hífen": {Host: "h", Port: 1, Username: "u", Password: "p", Database: "--all-databases"},
	} {
		if _, err := mysqlTargetFrom(info); err == nil {
			t.Errorf("%s: quero erro", name)
		}
	}
}

func TestMySQLOptionsFileContent(t *testing.T) {
	cases := []struct{ password, want string }{
		{"s3cretPassw0rd", "[client]\npassword=\"s3cretPassw0rd\"\n"},
		{`a"b`, "[client]\npassword=\"a\\\"b\"\n"},
		{`a\b`, "[client]\npassword=\"a\\\\b\"\n"},
		{"a#b c", "[client]\npassword=\"a#b c\"\n"},
		{"tab\there\nnl", "[client]\npassword=\"tab\\there\\nnl\"\n"},
	}
	for _, tc := range cases {
		if got := mysqlOptionsFileContent(tc.password); got != tc.want {
			t.Errorf("content(%q) = %q, quero %q", tc.password, got, tc.want)
		}
	}
}

func TestNewMySQLSecretsPermissionsAndCleanup(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "corp.pem")
	if err := os.WriteFile(bundle, []byte("pem"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(cabundle.EnvCertFile, bundle)

	s, err := newMySQLSecrets(testMySQLPassword)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.OptionsFile)
	if err != nil || string(data) != mysqlOptionsFileContent(testMySQLPassword) {
		t.Fatalf("options file = %q (%v)", data, err)
	}
	if filepath.Dir(s.OptionsFile) != s.Dir {
		t.Fatalf("options file fora do diretório privado: %s", s.OptionsFile)
	}
	if s.CAFile != bundle || s.CASource != cabundle.SourceEnv {
		t.Fatalf("CA = %s (%s), quero o SSL_CERT_FILE", s.CAFile, s.CASource)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(s.OptionsFile)
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("options file %o, quero 0600", perm)
		}
		di, _ := os.Stat(s.Dir)
		if perm := di.Mode().Perm(); perm != 0o700 {
			t.Fatalf("diretório %o, quero 0700", perm)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatalf("diretório não removido: %v", err)
	}
}

// Sem SSL_CERT_FILE e sem bundle do sistema (Windows), o bundle embutido é
// gravado no diretório privado e sai junto com a senha.
func TestNewMySQLSecretsEmbeddedBundle(t *testing.T) {
	t.Setenv(cabundle.EnvCertFile, "")
	prev := cabundle.KnownPaths
	cabundle.KnownPaths = []string{filepath.Join(t.TempDir(), "missing.pem")}
	t.Cleanup(func() { cabundle.KnownPaths = prev })

	s, err := newMySQLSecrets(testMySQLPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if s.CASource != cabundle.SourceEmbedded || filepath.Dir(s.CAFile) != s.Dir {
		t.Fatalf("CA = %s (%s), quero o embutido dentro de %s", s.CAFile, s.CASource, s.Dir)
	}
	data, _ := os.ReadFile(s.CAFile)
	if string(data) != string(cabundle.EmbeddedPEM()) {
		t.Fatal("conteúdo do bundle embutido difere")
	}
}

// ─── execução com segredo: arquivo durante o processo, nada depois ───────────

type helperReport struct {
	Args    []string `json:"args"`
	Mode    string   `json:"mode"`
	Options string   `json:"options"`
	Env     []string `json:"env"`
}

// TestMySQLHelperProcess não é um teste: é o "cliente mysql" falso que os
// testes executam (o próprio binário de teste). Inspeciona o que recebeu e
// grava um relatório.
func TestMySQLHelperProcess(t *testing.T) {
	mode := os.Getenv("UPUAI_MYSQL_HELPER")
	if mode == "" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	rep := helperReport{Args: args, Env: os.Environ()}
	for _, a := range args {
		if strings.HasPrefix(a, mysqlOptionsFileFlag+"=") {
			path := strings.TrimPrefix(a, mysqlOptionsFileFlag+"=")
			if fi, err := os.Stat(path); err == nil {
				rep.Mode = fmt.Sprintf("%o", fi.Mode().Perm())
			}
			data, _ := os.ReadFile(path)
			rep.Options = string(data)
		}
	}
	raw, _ := json.Marshal(rep)
	_ = os.WriteFile(os.Getenv("UPUAI_MYSQL_HELPER_OUT"), raw, 0o600)

	switch mode {
	case "exit":
		code, _ := strconv.Atoi(os.Getenv("UPUAI_MYSQL_HELPER_EXIT"))
		os.Exit(code)
	case "wait-term":
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM)
		_ = os.WriteFile(os.Getenv("UPUAI_MYSQL_HELPER_OUT")+".ready", nil, 0o600)
		select {
		case <-ch:
			os.Exit(143)
		case <-time.After(10 * time.Second):
			os.Exit(99)
		}
	case "sleep":
		time.Sleep(300 * time.Millisecond)
	}
	os.Exit(0)
}

// helperCommand monta o processo auxiliar com o argv real do mysql.
func helperCommand(t *testing.T, mode string, extraEnv ...string) (func(s *mysqlSecrets) *exec.Cmd, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "report.json")
	target := mysqlTarget{Host: "orders.db.upuai.cloud", Port: 23307, User: "app", Database: "app"}
	return func(s *mysqlSecrets) *exec.Cmd {
		args := append([]string{"-test.run=^TestMySQLHelperProcess$", "--"}, mysqlClientArgs(mysqlFlavorOracle, s.OptionsFile, s.CAFile, target)...)
		c := exec.Command(os.Args[0], args...)
		c.Env = append(os.Environ(), append([]string{"UPUAI_MYSQL_HELPER=" + mode, "UPUAI_MYSQL_HELPER_OUT=" + out}, extraEnv...)...)
		return c
	}, out
}

func readHelperReport(t *testing.T, path string) helperReport {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relatório do helper: %v", err)
	}
	var rep helperReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

func optionsFileFromArgs(t *testing.T, args []string) string {
	t.Helper()
	for _, a := range args {
		if strings.HasPrefix(a, mysqlOptionsFileFlag+"=") {
			return strings.TrimPrefix(a, mysqlOptionsFileFlag+"=")
		}
	}
	t.Fatalf("sem %s em %q", mysqlOptionsFileFlag, args)
	return ""
}

func TestRunMySQLWithSecretsLifecycle(t *testing.T) {
	cases := []struct {
		name     string
		exit     int
		wantCode int
	}{
		{"cliente sai 0", 0, 0},
		{"cliente sai com erro", 7, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			build, out := helperCommand(t, "exit", "UPUAI_MYSQL_HELPER_EXIT="+strconv.Itoa(tc.exit))
			err := runMySQLWithSecrets(testMySQLPassword, build)
			var exitErr *exec.ExitError
			switch {
			case tc.wantCode == 0 && err != nil:
				t.Fatalf("err = %v", err)
			case tc.wantCode != 0 && (!errors.As(err, &exitErr) || exitErr.ExitCode() != tc.wantCode):
				t.Fatalf("err = %v, quero exit %d", err, tc.wantCode)
			}
			rep := readHelperReport(t, out)
			if rep.Options != mysqlOptionsFileContent(testMySQLPassword) {
				t.Fatalf("o cliente leu %q", rep.Options)
			}
			if runtime.GOOS != "windows" && rep.Mode != "600" {
				t.Fatalf("options file %s durante o processo, quero 600", rep.Mode)
			}
			for _, a := range rep.Args {
				if strings.Contains(a, testMySQLPassword) {
					t.Fatalf("senha no argv: %q", a)
				}
			}
			for _, e := range rep.Env {
				if strings.Contains(e, testMySQLPassword) {
					t.Fatalf("senha no ambiente: %q", e)
				}
			}
			opt := optionsFileFromArgs(t, rep.Args)
			if _, err := os.Stat(filepath.Dir(opt)); !os.IsNotExist(err) {
				t.Fatalf("diretório da credencial sobrou após o processo: %v", err)
			}
		})
	}
}

// Falha ao iniciar o cliente também limpa.
func TestRunMySQLWithSecretsCleansUpWhenStartFails(t *testing.T) {
	var dir string
	err := runMySQLWithSecrets(testMySQLPassword, func(s *mysqlSecrets) *exec.Cmd {
		dir = s.Dir
		return exec.Command(filepath.Join(t.TempDir(), "does-not-exist"))
	})
	if err == nil {
		t.Fatal("quero erro ao iniciar")
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatalf("diretório da credencial sobrou: %v", statErr)
	}
}

// ─── nome do backup, formato do dump ─────────────────────────────────────────

func TestMySQLBackupFileName(t *testing.T) {
	at := time.Date(2026, 10, 7, 21, 5, 9, 0, time.FixedZone("BRT", -3*3600))
	cases := []struct{ service, want string }{
		{"orders-db", "orders-db-20261008-000509.sql"},
		{"Orders DB", "Orders-DB-20261008-000509.sql"},
		{"../etc/passwd", "etc-passwd-20261008-000509.sql"},
		{"", "mysql-20261008-000509.sql"},
	}
	for _, tc := range cases {
		if got := mysqlBackupFileName(tc.service, at); got != tc.want {
			t.Errorf("mysqlBackupFileName(%q) = %q, quero %q", tc.service, got, tc.want)
		}
	}
}

func TestSniffDumpFormat(t *testing.T) {
	tarHead := make([]byte, 512)
	copy(tarHead, "toc.dat")
	copy(tarHead[257:], "ustar")
	otherTar := make([]byte, 512)
	copy(otherTar, "data.csv")
	copy(otherTar[257:], "ustar")

	cases := []struct {
		name string
		head []byte
		want dumpFormat
	}{
		{"pg_dump custom", []byte("PGDMP\x01\x0f\x00"), dumpFormatPostgresCustom},
		{"pg_dump tar", tarHead, dumpFormatPostgresTar},
		{"tar qualquer", otherTar, dumpFormatUnknown},
		{"pg_dump plain", []byte("--\n-- PostgreSQL database dump\n--\n"), dumpFormatPostgresSQL},
		{"mysqldump Oracle", []byte("-- MySQL dump 10.13  Distrib 8.4.3, for macos15.0 (arm64)\n--\n"), dumpFormatMySQLSQL},
		{"mysqldump MariaDB", []byte(mariadbSandboxLine + "\n-- MariaDB dump 10.19-11.4.2-MariaDB, for debian-linux-gnu (x86_64)\n"), dumpFormatMySQLSQL},
		{"gzip", []byte{0x1f, 0x8b, 0x08, 0x00}, dumpFormatGzip},
		{"SQL sem cabeçalho", []byte("CREATE TABLE t (id int);\n"), dumpFormatUnknown},
		{"vazio", nil, dumpFormatUnknown},
	}
	for _, tc := range cases {
		if got := sniffDumpFormat(tc.head); got != tc.want {
			t.Errorf("%s: %d, quero %d", tc.name, got, tc.want)
		}
	}
}

func TestCheckDumpMatchesEngine(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	pgCustom := write("db.dump", "PGDMP\x01\x0f\x00rest")
	pgPlain := write("pg.sql", "--\n-- PostgreSQL database dump\n--\n")
	myDump := write("my.sql", "-- MySQL dump 10.13  Distrib 8.4.3\n")
	plain := write("plain.sql", "INSERT INTO t VALUES (1);\n")
	gz := write("my.sql.gz", "\x1f\x8b\x08\x00")
	pgDir := filepath.Join(dir, "pgdir")
	if err := os.Mkdir(pgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	write("pgdir/toc.dat", "PGDMP")

	mysql := &api.PublicAccessInfo{Engine: api.DatabaseEngineMySQL}
	pg := &api.PublicAccessInfo{Engine: api.DatabaseEnginePostgres}
	cases := []struct {
		name    string
		path    string
		info    *api.PublicAccessInfo
		wantErr string
	}{
		{"MySQL recusa pg_dump custom", pgCustom, mysql, "PostgreSQL dump"},
		{"MySQL recusa pg_dump plain", pgPlain, mysql, "PostgreSQL dump"},
		{"MySQL recusa pg_dump directory", pgDir, mysql, "PostgreSQL dump"},
		{"MySQL recusa gzip", gz, mysql, "gunzip"},
		{"MySQL aceita mysqldump", myDump, mysql, ""},
		{"MySQL aceita SQL sem cabeçalho", plain, mysql, ""},
		{"Postgres recusa mysqldump", myDump, pg, "MySQL dump"},
		{"Postgres aceita custom", pgCustom, pg, ""},
		{"Postgres segue como antes com plain", pgPlain, pg, ""},
		{"Postgres segue como antes com directory", pgDir, pg, ""},
	}
	for _, tc := range cases {
		err := checkDumpMatchesEngine(tc.path, tc.info)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: err = %v", tc.name, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s: err = %v, quero %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestMySQLRestoreReader(t *testing.T) {
	body := "-- MariaDB dump 10.19\nCREATE TABLE t (id int);\n"
	sandboxed := mariadbSandboxLine + "\n" + body
	cases := []struct {
		name   string
		in     string
		flavor mysqlFlavor
		want   string
	}{
		{"Oracle descarta a linha de sandbox do MariaDB", sandboxed, mysqlFlavorOracle, body},
		{"MariaDB mantém a linha", sandboxed, mysqlFlavorMariaDB, sandboxed},
		{"dump sem a linha passa intacto", body, mysqlFlavorOracle, body},
		{"linha parecida no meio não é tocada", body + mariadbSandboxLine + "\n", mysqlFlavorOracle, body + mariadbSandboxLine + "\n"},
	}
	for _, tc := range cases {
		got, err := io.ReadAll(mysqlRestoreReader(strings.NewReader(tc.in), tc.flavor))
		if err != nil || string(got) != tc.want {
			t.Errorf("%s: %q (%v), quero %q", tc.name, got, err, tc.want)
		}
	}
}

// ─── espera do certificado ───────────────────────────────────────────────────

func TestWaitForServerTLS(t *testing.T) {
	ready := &api.PublicAccessInfo{Engine: "mysql", Enabled: true, ServerTLSReady: true}
	pending := &api.PublicAccessInfo{Engine: "mysql", Enabled: true}
	notReady := &api.APIError{StatusCode: 409, Code: "DB_NOT_READY"}
	forbidden := &api.APIError{StatusCode: 403, Message: "nope"}

	type step struct {
		info *api.PublicAccessInfo
		err  error
	}
	cases := []struct {
		name      string
		steps     []step // o último se repete
		wantReady bool
		wantErr   string
		maxPolls  int
	}{
		{"false,false,true", []step{{pending, nil}, {pending, nil}, {ready, nil}}, true, "", 3},
		{"DB_NOT_READY durante o restart é esperar", []step{{nil, notReady}, {nil, errors.New("connection reset")}, {ready, nil}}, true, "", 3},
		{"nunca pronto: timeout", []step{{pending, nil}}, false, "db public status", 0},
		{"403 para na hora", []step{{nil, forbidden}}, false, "nope", 1},
		{"desligado no meio", []step{{pending, nil}, {&api.PublicAccessInfo{Engine: "mysql"}, nil}}, false, "disabled", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			polls := 0
			got, err := waitForServerTLS(func() (*api.PublicAccessInfo, error) {
				s := tc.steps[min(polls, len(tc.steps)-1)]
				polls++
				return s.info, s.err
			}, time.Millisecond, 30*time.Millisecond)
			if tc.wantReady {
				if err != nil || got == nil || !got.ServerTLSReady {
					t.Fatalf("got %+v, %v", got, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, quero %q", err, tc.wantErr)
			}
			if tc.maxPolls > 0 && polls != tc.maxPolls {
				t.Fatalf("polls = %d, quero %d", polls, tc.maxPolls)
			}
		})
	}
}

// ─── sonda da rota ───────────────────────────────────────────────────────────

// fakeMySQLEdge: as primeiras `drops` conexões fecham sem resposta (Traefik
// ainda sem rota); depois responde com o início de um handshake v10.
func fakeMySQLEdge(t *testing.T, drops int) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for n := 0; ; n++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if drops < 0 || n < drops {
				_ = conn.Close()
				continue
			}
			_, _ = conn.Write([]byte{0x4a, 0x00, 0x00, 0x00, 0x0a, '8', '.', '4'})
			_ = conn.Close()
		}
	}()
	return ln.Addr().String()
}

func TestWaitForMySQLGreeting(t *testing.T) {
	if !waitForMySQLGreeting(fakeMySQLEdge(t, 2), 2*time.Second, time.Millisecond) {
		t.Fatal("quero a saudação depois de duas conexões recusadas")
	}
	if waitForMySQLGreeting(fakeMySQLEdge(t, -1), 50*time.Millisecond, time.Millisecond) {
		t.Fatal("sem saudação a sonda não pode dar a rota como pronta")
	}
}

// ─── tradução de erros ───────────────────────────────────────────────────────

func TestExplainPublicAccessError(t *testing.T) {
	forbidden := &api.APIError{StatusCode: 403, Message: "This API token is read-only (missing DEPLOY scope)", RequestID: "req-1"}
	roleForbidden := &api.APIError{StatusCode: 403, Message: "Insufficient permissions"}
	cases := []struct {
		name    string
		token   string
		err     error
		write   bool
		want    string
		wantRaw bool
	}{
		{"GET com token READ", "upua_ro", forbidden, false, "this token can read but public access returns credentials — use a token with deploy scope", false},
		{"PUT com token READ", "upua_ro", forbidden, true, "this token is read-only — changing public access needs a token with deploy scope", false},
		{"PUT de usuário sem papel", "", roleForbidden, true, "requires the owner or admin role", false},
		{"GET de usuário 403 segue cru", "", roleForbidden, false, "API error 403", true},
		{"motor sem acesso público", "", &api.APIError{StatusCode: 409, Code: "PUBLIC_ACCESS_UNSUPPORTED_ENGINE"}, true, "PostgreSQL and MySQL databases only", false},
		{"pool indisponível", "", &api.APIError{StatusCode: 409, Code: "PUBLIC_ACCESS_UNAVAILABLE"}, true, "not available on this cluster", false},
		{"pool esgotado", "", &api.APIError{StatusCode: 409, Code: "PUBLIC_PORT_POOL_EXHAUSTED"}, true, "no public port is free", false},
		{"porta em conflito", "", &api.APIError{StatusCode: 409, Code: "PUBLIC_PORT_CONFLICT"}, true, "routed to another database", false},
		{"porta inválida", "", &api.APIError{StatusCode: 500, Code: "PUBLIC_PORT_INVALID"}, true, "invalid public port", false},
		{"banco não pronto", "", &api.APIError{StatusCode: 409, Code: "DB_NOT_READY"}, true, "not up yet", false},
		{"operação em curso", "", &api.APIError{StatusCode: 409, Code: "DB_OPERATION_IN_PROGRESS"}, true, "another operation", false},
		{"código desconhecido mantém o contexto", "", &api.APIError{StatusCode: 500, Message: "boom"}, true, "enable public access: API error 500: boom", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(config.EnvTokenVar, tc.token)
			err := explainPublicAccessError(tc.err, "enable public access", tc.write)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, quero %q", err, tc.want)
			}
			if api.StatusCode(err) != api.StatusCode(tc.err) {
				t.Fatalf("a causa se perdeu: %v", err)
			}
			if !tc.wantRaw && strings.Contains(err.Error(), "API error") {
				t.Fatalf("mensagem crua: %v", err)
			}
		})
	}
	t.Setenv(config.EnvTokenVar, "upua_ro")
	if err := explainPublicAccessError(forbidden, "get public access", false); !strings.Contains(err.Error(), "requestId: req-1") {
		t.Fatalf("requestId perdido: %v", err)
	}
}

func TestExplainMySQLToolError(t *testing.T) {
	info := &api.PublicAccessInfo{Host: "orders.db.upuai.cloud", Port: 23307}
	tool := mysqlTool{Name: "mysqldump", Path: "/usr/bin/mysqldump", Flavor: mysqlFlavorOracle}
	cause := errors.New("exit status 2")
	cases := []struct{ stderr, want string }{
		{"mysqldump: Got error: 1045: Access denied for user 'app'@'203.0.113.7' (using password: YES)", "credentials repair"},
		{"ERROR 2026 (HY000): SSL connection error: error:0A000086:SSL routines::certificate verify failed", "SSL_CERT_FILE"},
		{"ERROR 2026 (HY000): TLS/SSL error: Certificate verification failure", "SSL_CERT_FILE"},
		{"ERROR 2013 (HY000): Lost connection to MySQL server at 'reading initial communication packet', system error: 0", "orders.db.upuai.cloud:23307"},
		{"ERROR 2003 (HY000): Can't connect to MySQL server on 'orders.db.upuai.cloud:23307' (61)", "public status"},
		{"ERROR 1227 (42000) at line 18: Access denied; you need (at least one of) the SUPER, SYSTEM_VARIABLES_ADMIN or SESSION_VARIABLES_ADMIN privilege(s) for this operation", "--set-gtid-purged=OFF"},
		{"ERROR 2059 (HY000): Authentication plugin 'caching_sha2_password' cannot be loaded", "brew install mysql-client"},
		{"mysqldump: unknown variable 'set-gtid-purged=OFF'", "--version"},
		{"something else", "mysqldump failed: exit status 2"},
	}
	for _, tc := range cases {
		err := explainMySQLToolError(tool, cause, tc.stderr, info)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("stderr %q → %v, quero %q", tc.stderr, err, tc.want)
		}
	}
}

func TestTailBuffer(t *testing.T) {
	b := newTailBuffer(8)
	_, _ = b.Write([]byte("0123456789"))
	_, _ = b.Write([]byte("ab"))
	if got := b.String(); got != "456789ab" {
		t.Fatalf("tail = %q", got)
	}
}

// ─── comandos ────────────────────────────────────────────────────────────────

// Postgres de uma API antiga (sem engine): a saída é exatamente a de antes.
func TestDBPublicStatusPostgresUnchanged(t *testing.T) {
	newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 200, pgLegacyJSON
	})
	var err error
	out := captureStdout(t, func() { err = dbPublicStatusCmd.RunE(dbPublicStatusCmd, nil) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Public access", "enabled", "Host", "abc123xy.db.upuai.cloud", "Port", "5432", "Allowed from", "203.0.113.0/24"} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"Engine", "User", "TLS", "db connect"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("saída do Postgres mudou (%q):\n%s", unwanted, out)
		}
	}
	if lines := strings.Count(strings.TrimSpace(out), "\n") + 1; lines != 4 {
		t.Errorf("Postgres imprimia 4 linhas, agora %d:\n%s", lines, out)
	}
}

func TestDBPublicStatusMySQL(t *testing.T) {
	newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 200, mysqlAccessJSON(true, true)
	})
	var err error
	out := captureStdout(t, func() { err = dbPublicCmd.RunE(dbPublicCmd, nil) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Engine", "MySQL", "orders.db.upuai.cloud", "23307", "User", "app", "Database", "any IP", "ready", "upuai db connect"} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, testMySQLPassword) {
		t.Fatalf("senha na saída humana:\n%s", out)
	}
}

func TestDBPublicStatusMySQLDisabledWarnsRestart(t *testing.T) {
	newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 200, mysqlAccessJSON(false, false)
	})
	var err error
	out := captureStdout(t, func() { err = dbPublicStatusCmd.RunE(dbPublicStatusCmd, nil) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "disabled") || !strings.Contains(out, "restarts this MySQL once") {
		t.Fatalf("saída = %q", out)
	}
}

// Sem TTY e sem --yes, ligar um MySQL que reinicia é recusado ANTES do PUT.
func TestDBPublicEnableMySQLRestartNeedsYes(t *testing.T) {
	reqs := newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 200, mysqlAccessJSON(false, false)
	})
	dbAllowCIDRs = []string{"203.0.113.7"}
	var err error
	captureStdout(t, func() { err = dbPublicEnableCmd.RunE(dbPublicEnableCmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "--yes") || !strings.Contains(err.Error(), "restarts") {
		t.Fatalf("err = %v", err)
	}
	if countMethod(*reqs, http.MethodPut) != 0 {
		t.Fatalf("PUT sem confirmação: %+v", *reqs)
	}
}

// --yes: liga, espera o restart (GET false,false,true) e mostra o banco pronto.
func TestDBPublicEnableMySQLWaitsForTLS(t *testing.T) {
	reqs := newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		switch {
		case method == http.MethodPut:
			return 200, mysqlAccessJSON(true, false)
		case n == 0: // estado antes de ligar
			return 200, mysqlAccessJSON(false, false)
		case n < 3:
			return 200, mysqlAccessJSON(true, false)
		default:
			return 200, mysqlAccessJSON(true, true)
		}
	})
	flagYes, dbAllowAny = true, true
	var err error
	out := captureStdout(t, func() { err = dbPublicEnableCmd.RunE(dbPublicEnableCmd, nil) })
	if err != nil {
		t.Fatal(err)
	}
	if got := countMethod(*reqs, http.MethodGet); got != 4 {
		t.Fatalf("GETs = %d, quero 1 antes + 3 de espera", got)
	}
	var put publicAccessPut
	for _, r := range *reqs {
		if r.Method == http.MethodPut {
			_ = json.Unmarshal([]byte(r.Body), &put)
		}
	}
	if !put.Enabled || put.AllowedCidrs == nil || len(put.AllowedCidrs) != 0 {
		t.Fatalf("PUT = %+v", put)
	}
	for _, want := range []string{"public access enabled", "ready", "23307", "upuai db connect"} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
}

func TestDBPublicEnableMySQLTLSTimeout(t *testing.T) {
	newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		if method == http.MethodGet && n == 0 {
			return 200, mysqlAccessJSON(false, false)
		}
		return 200, mysqlAccessJSON(true, false)
	})
	flagYes, dbAllowAny = true, true
	mysqlTLSWaitTimeout = 20 * time.Millisecond
	var err error
	captureStdout(t, func() { err = dbPublicEnableCmd.RunE(dbPublicEnableCmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "still restarting") || !strings.Contains(err.Error(), "upuai db public status") {
		t.Fatalf("err = %v", err)
	}
}

// Postgres: enable continua um PUT, sem espera de TLS (serverTlsReady ausente
// numa API antiga não pode disparar o polling).
func TestDBPublicEnablePostgresNoTLSWait(t *testing.T) {
	reqs := newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 200, pgLegacyJSON
	})
	flagYes = true
	var err error
	out := captureStdout(t, func() { err = dbPublicEnableCmd.RunE(dbPublicEnableCmd, nil) })
	if err != nil {
		t.Fatal(err)
	}
	if countMethod(*reqs, http.MethodGet) != 1 || countMethod(*reqs, http.MethodPut) != 1 {
		t.Fatalf("requests = %+v", *reqs)
	}
	if strings.Contains(out, "TLS") || strings.Contains(out, "Engine") {
		t.Fatalf("saída do Postgres mudou:\n%s", out)
	}
}

func TestDBConnectPrintMySQL(t *testing.T) {
	newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 200, mysqlAccessJSON(true, true)
	})
	dbConnectPrint = true
	var err error
	out := captureStdout(t, func() { err = dbConnectCmd.RunE(dbConnectCmd, nil) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Host", "orders.db.upuai.cloud", "Port", "23307", "User", "app", "Database",
		"mysql://app:" + testMySQLPassword + "@orders.db.upuai.cloud:23307/app?ssl-mode=VERIFY_IDENTITY"} {
		if !strings.Contains(out, want) {
			t.Errorf("saída sem %q:\n%s", want, out)
		}
	}
}

func TestDBConnectPrintPostgresUnchanged(t *testing.T) {
	newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 200, pgLegacyJSON
	})
	dbConnectPrint = true
	var err error
	out := captureStdout(t, func() { err = dbConnectCmd.RunE(dbConnectCmd, nil) })
	if err != nil {
		t.Fatal(err)
	}
	want := "postgresql://app:pw@abc123xy.db.upuai.cloud:5432/app?sslmode=verify-full"
	if !strings.Contains(out, want) || strings.Contains(out, "User") || strings.Contains(out, "Database") {
		t.Fatalf("saída:\n%s", out)
	}
}

// Sem o cliente local, nada é publicado (nem o MySQL reiniciado).
func TestDBConnectMySQLMissingClientDoesNotEnable(t *testing.T) {
	reqs := newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 200, mysqlAccessJSON(false, false)
	})
	t.Setenv("PATH", t.TempDir())
	flagYes = true
	var err error
	captureStdout(t, func() { err = dbConnectCmd.RunE(dbConnectCmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "mysql not found") {
		t.Fatalf("err = %v", err)
	}
	if countMethod(*reqs, http.MethodPut) != 0 {
		t.Fatalf("publicou sem cliente: %+v", *reqs)
	}
}

func TestDBBackupMySQLMissingDumpDoesNotEnable(t *testing.T) {
	reqs := newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 200, mysqlAccessJSON(false, true)
	})
	t.Setenv("PATH", t.TempDir())
	flagYes = true
	var err error
	captureStdout(t, func() { err = dbBackupCmd.RunE(dbBackupCmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "mysqldump not found") {
		t.Fatalf("err = %v", err)
	}
	if countMethod(*reqs, http.MethodPut) != 0 {
		t.Fatalf("publicou sem mysqldump: %+v", *reqs)
	}
}

func TestDBBackupPostgresStillRequiresOut(t *testing.T) {
	reqs := newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 200, pgDisabledLegacyJSON
	})
	flagYes = true
	err := dbBackupCmd.RunE(dbBackupCmd, nil)
	if err == nil || err.Error() != "--out is required (path to .dump file)" {
		t.Fatalf("err = %v", err)
	}
	if countMethod(*reqs, http.MethodPut) != 0 {
		t.Fatalf("publicou sem --out: %+v", *reqs)
	}
}

func TestDBRestoreRefusesWrongEngineBeforeEnabling(t *testing.T) {
	dir := t.TempDir()
	pgDump := filepath.Join(dir, "db.dump")
	myDump := filepath.Join(dir, "db.sql")
	_ = os.WriteFile(pgDump, []byte("PGDMP\x01"), 0o600)
	_ = os.WriteFile(myDump, []byte("-- MySQL dump 10.13\n"), 0o600)

	cases := []struct {
		name, file, state, want string
	}{
		{"pg_dump num MySQL", pgDump, mysqlAccessJSON(false, false), "PostgreSQL dump"},
		{"mysqldump num Postgres", myDump, pgDisabledLegacyJSON, "MySQL dump"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqs := newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
				return 200, tc.state
			})
			flagYes = true
			var err error
			captureStdout(t, func() { err = dbRestoreCmd.RunE(dbRestoreCmd, []string{tc.file}) })
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
			if countMethod(*reqs, http.MethodPut) != 0 {
				t.Fatalf("publicou o banco antes de recusar: %+v", *reqs)
			}
		})
	}
}

func TestDBRestoreFileArgumentAndFlagConflict(t *testing.T) {
	newPublicAccessFake(t, func(method string, n int, body string) (int, string) { return 200, pgLegacyJSON })
	dbRestoreIn = "a.dump"
	err := dbRestoreCmd.RunE(dbRestoreCmd, []string{"b.dump"})
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("err = %v", err)
	}
}

// GET com token READ: a mensagem diz o que fazer, não "read-only".
func TestDBPublicStatusReadTokenForbidden(t *testing.T) {
	newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 403, `{"statusCode":403,"error":"ForbiddenError","message":"This API token is read-only (missing DEPLOY scope)"}`
	})
	err := dbPublicStatusCmd.RunE(dbPublicStatusCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "this token can read but public access returns credentials — use a token with deploy scope") {
		t.Fatalf("err = %v", err)
	}
}

// O código chega em `error` (envelope de cluster) e vira mensagem.
func TestDBPublicEnableUnsupportedEngine(t *testing.T) {
	newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		if method == http.MethodPut {
			return 409, `{"statusCode":409,"error":"PUBLIC_ACCESS_UNSUPPORTED_ENGINE","message":"x","details":{"actionable":false,"retryable":false}}`
		}
		return 200, pgDisabledLegacyJSON
	})
	flagYes = true
	var err error
	captureStdout(t, func() { err = dbPublicEnableCmd.RunE(dbPublicEnableCmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "PostgreSQL and MySQL databases only") {
		t.Fatalf("err = %v", err)
	}
}
