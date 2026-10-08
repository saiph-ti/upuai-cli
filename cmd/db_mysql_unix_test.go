//go:build !windows

package cmd

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeDBTool é um cliente de banco falso (shell): responde --version com
// $FAKE_VERSION e, executado de verdade, registra argv, uma cópia e o modo do
// arquivo de opções, e o stdin (restore); o dump vai para o stdout.
const fakeDBTool = `#!/bin/sh
if [ "$1" = "--version" ]; then echo "$FAKE_VERSION"; exit 0; fi
printf '%s\n' "$@" > "$FAKE_DIR/args"
for a in "$@"; do
  case "$a" in
    --defaults-file=*) opt="${a#--defaults-file=}";;
  esac
done
if [ -n "$opt" ]; then
  cat "$opt" > "$FAKE_DIR/options"
  ls -ln "$opt" | cut -c1-10 > "$FAKE_DIR/mode"
fi
case "$(basename "$0")" in
  mysqldump|mariadb-dump|pg_dump) printf -- "$FAKE_DUMP" ;;
  mysql|mariadb|pg_restore) cat > "$FAKE_DIR/stdin" ;;
esac
if [ -n "$FAKE_STDERR" ]; then echo "$FAKE_STDERR" >&2; fi
exit "${FAKE_EXIT:-0}"
`

// installFakeDBTools põe os binários falsos à frente do PATH.
func installFakeDBTools(t *testing.T, names ...string) string {
	t.Helper()
	bin := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(bin, n), []byte(fakeDBTool), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	record := t.TempDir()
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("FAKE_DIR", record)
	t.Setenv("FAKE_EXIT", "0")
	t.Setenv("FAKE_STDERR", "")
	return record
}

func readRecorded(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("%s não registrado: %v", name, err)
	}
	return string(data)
}

func recordedArgs(t *testing.T, dir string) []string {
	return strings.Split(strings.TrimRight(readRecorded(t, dir, "args"), "\n"), "\n")
}

// assertSecretHandling: senha só no arquivo 0600, nunca no argv, e o
// diretório some depois.
func assertSecretHandling(t *testing.T, record string, args []string) {
	t.Helper()
	if !strings.HasPrefix(args[0], "--defaults-file=") {
		t.Fatalf("primeiro argumento = %q", args[0])
	}
	if got := readRecorded(t, record, "options"); got != mysqlOptionsFileContent(testMySQLPassword) {
		t.Fatalf("options = %q", got)
	}
	if got := strings.TrimSpace(readRecorded(t, record, "mode")); got != "-rw-------" {
		t.Fatalf("modo do options file = %q, quero -rw-------", got)
	}
	for _, a := range args {
		if strings.Contains(a, testMySQLPassword) {
			t.Fatalf("senha no argv: %q", a)
		}
	}
	opt := strings.TrimPrefix(args[0], "--defaults-file=")
	if _, err := os.Stat(filepath.Dir(opt)); !os.IsNotExist(err) {
		t.Fatalf("credencial sobrou em %s: %v", filepath.Dir(opt), err)
	}
}

func caArg(t *testing.T, args []string) string {
	t.Helper()
	for _, a := range args {
		if strings.HasPrefix(a, "--ssl-ca=") {
			return strings.TrimPrefix(a, "--ssl-ca=")
		}
	}
	t.Fatalf("sem --ssl-ca em %q", args)
	return ""
}

func TestDBBackupMySQLEndToEnd(t *testing.T) {
	cases := []struct {
		name       string
		tool       string
		version    string
		wantTLS    []string
		wantGTID   bool
		notWantTLS string
	}{
		{"Oracle", "mysqldump", "mysqldump  Ver 8.4.3 for macos15.0 on arm64 (Homebrew)", []string{"--ssl-mode=VERIFY_IDENTITY"}, true, "--ssl-verify-server-cert"},
		{"MariaDB como mysqldump", "mysqldump", "mysqldump  Ver 10.19 Distrib 10.11.8-MariaDB, for debian-linux-gnu (x86_64)", []string{"--ssl", "--ssl-verify-server-cert"}, false, "--ssl-mode=VERIFY_IDENTITY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqs := newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
				return 200, mysqlAccessJSON(true, true)
			})
			record := installFakeDBTools(t, tc.tool)
			t.Setenv("FAKE_VERSION", tc.version)
			t.Setenv("FAKE_DUMP", `-- MySQL dump 10.13\nCREATE TABLE t (id int);\n`)

			var err error
			out := captureStdout(t, func() { err = dbBackupCmd.RunE(dbBackupCmd, nil) })
			if err != nil {
				t.Fatalf("backup: %v\n%s", err, out)
			}
			args := recordedArgs(t, record)
			assertSecretHandling(t, record, args)
			for _, want := range append(tc.wantTLS, "--host=orders.db.upuai.cloud", "--port=23307", "--user=app",
				"--single-transaction", "--routines", "--events", "--triggers", "--no-tablespaces") {
				if !slices.Contains(args, want) {
					t.Errorf("argv sem %q: %q", want, args)
				}
			}
			if slices.Contains(args, "--set-gtid-purged=OFF") != tc.wantGTID {
				t.Errorf("--set-gtid-purged=OFF presente=%t, quero %t: %q", !tc.wantGTID, tc.wantGTID, args)
			}
			if slices.Contains(args, tc.notWantTLS) {
				t.Errorf("flag do outro sabor %q: %q", tc.notWantTLS, args)
			}
			if args[len(args)-1] != "app" {
				t.Errorf("banco não é o último argumento: %q", args)
			}
			if ca := caArg(t, args); ca == "" {
				t.Error("--ssl-ca vazio")
			}

			// Arquivo padrão: <slug>-<UTC>.sql no diretório atual.
			matches, _ := filepath.Glob("orders-db-*.sql")
			if len(matches) != 1 {
				t.Fatalf("arquivo de backup = %v", matches)
			}
			data, _ := os.ReadFile(matches[0])
			if !strings.HasPrefix(string(data), "-- MySQL dump") {
				t.Fatalf("conteúdo = %q", data)
			}
			fi, _ := os.Stat(matches[0])
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("backup %o, quero 0600", fi.Mode().Perm())
			}
			if !strings.Contains(out, "backup written: "+matches[0]) {
				t.Errorf("saída = %q", out)
			}
			if countMethod(*reqs, http.MethodPut) != 0 {
				t.Errorf("banco já publicado não pede PUT: %+v", *reqs)
			}
			// Já publicado e pronto: nada a esperar na rota.
			if len(probedAddrs) != 0 {
				t.Errorf("sonda sem motivo: %q", probedAddrs)
			}
		})
	}
}

// Falha do mysqldump: arquivo incompleto removido, credencial removida, erro
// traduzido.
func TestDBBackupMySQLFailureRemovesPartialFile(t *testing.T) {
	newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		return 200, mysqlAccessJSON(true, true)
	})
	record := installFakeDBTools(t, "mysqldump")
	t.Setenv("FAKE_VERSION", "mysqldump  Ver 8.4.3 for Linux on x86_64")
	t.Setenv("FAKE_DUMP", `-- MySQL dump 10.13\nCREATE TAB`)
	t.Setenv("FAKE_EXIT", "2")
	t.Setenv("FAKE_STDERR", "mysqldump: Got error: 1045: Access denied for user 'app'@'203.0.113.7' (using password: YES)")
	dbBackupOut = "out.sql"

	var err error
	captureStdout(t, func() { err = dbBackupCmd.RunE(dbBackupCmd, nil) })
	if err == nil || !strings.Contains(err.Error(), "credentials repair") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat("out.sql"); !os.IsNotExist(statErr) {
		t.Fatalf("dump incompleto ficou: %v", statErr)
	}
	assertSecretHandling(t, record, recordedArgs(t, record))
}

// Restore: o .sql vai por stdin (stream); a linha de sandbox de um dump do
// MariaDB sai quando o cliente é o da Oracle.
func TestDBRestoreMySQLEndToEnd(t *testing.T) {
	body := "-- MariaDB dump 10.19-11.4.2-MariaDB\nCREATE TABLE t (id int);\nINSERT INTO t VALUES (1);\n"
	cases := []struct {
		name, version, wantStdin string
		wantTLS                  string
	}{
		{"Oracle", "mysql  Ver 8.4.3 for macos15.0 on arm64 (Homebrew)", body, "--ssl-mode=VERIFY_IDENTITY"},
		{"MariaDB", "mysql  Ver 15.1 Distrib 10.6.18-MariaDB, for debian-linux-gnu (x86_64)", mariadbSandboxLine + "\n" + body, "--ssl-verify-server-cert"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dump := filepath.Join(t.TempDir(), "backup.sql")
			if err := os.WriteFile(dump, []byte(mariadbSandboxLine+"\n"+body), 0o600); err != nil {
				t.Fatal(err)
			}
			reqs := newPublicAccessFake(t, func(method string, n int, b string) (int, string) {
				return 200, mysqlAccessJSON(true, true)
			})
			record := installFakeDBTools(t, "mysql")
			t.Setenv("FAKE_VERSION", tc.version)
			flagYes = true

			var err error
			out := captureStdout(t, func() { err = dbRestoreCmd.RunE(dbRestoreCmd, []string{dump}) })
			if err != nil {
				t.Fatalf("restore: %v\n%s", err, out)
			}
			if got := readRecorded(t, record, "stdin"); got != tc.wantStdin {
				t.Fatalf("stdin = %q, quero %q", got, tc.wantStdin)
			}
			args := recordedArgs(t, record)
			assertSecretHandling(t, record, args)
			if !slices.Contains(args, tc.wantTLS) || !slices.Contains(args, "--database=app") {
				t.Fatalf("argv = %q", args)
			}
			if !strings.Contains(out, "restore complete") {
				t.Fatalf("saída = %q", out)
			}
			if countMethod(*reqs, http.MethodPut) != 0 {
				t.Fatalf("PUT inesperado: %+v", *reqs)
			}
		})
	}
}

// Connect auto-habilitando um MySQL antigo: --yes, PUT, espera do TLS, sonda
// da rota e o cliente com o argv verificado.
func TestDBConnectMySQLEnablesWaitsAndRuns(t *testing.T) {
	reqs := newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
		switch {
		case method == http.MethodPut:
			return 200, mysqlAccessJSON(true, false)
		case n == 0:
			return 200, mysqlAccessJSON(false, false)
		case n == 1:
			return 409, `{"statusCode":409,"error":"DB_NOT_READY","message":"restarting"}`
		default:
			return 200, mysqlAccessJSON(true, true)
		}
	})
	record := installFakeDBTools(t, "mysql")
	t.Setenv("FAKE_VERSION", "mysql  Ver 8.4.3 for Linux on x86_64 (MySQL Community Server - GPL)")
	flagYes = true

	start := time.Now()
	var err error
	out := captureStdout(t, func() { err = dbConnectCmd.RunE(dbConnectCmd, nil) })
	if err != nil {
		t.Fatalf("connect: %v\n%s", err, out)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("connect demorou %s", time.Since(start))
	}
	if countMethod(*reqs, http.MethodPut) != 1 || countMethod(*reqs, http.MethodGet) != 3 {
		t.Fatalf("requests = %+v", *reqs)
	}
	// Recém-publicado: a sonda espera a saudação do servidor antes do cliente.
	if !slices.Equal(probedAddrs, []string{"orders.db.upuai.cloud:23307"}) {
		t.Fatalf("sonda = %q", probedAddrs)
	}
	args := recordedArgs(t, record)
	assertSecretHandling(t, record, args)
	want := []string{"--host=orders.db.upuai.cloud", "--port=23307", "--user=app", "--ssl-mode=VERIFY_IDENTITY", "--database=app"}
	for _, w := range want {
		if !slices.Contains(args, w) {
			t.Fatalf("argv sem %q: %q", w, args)
		}
	}
	if !strings.Contains(out, "opening mysql → orders.db.upuai.cloud:23307") {
		t.Fatalf("saída = %q", out)
	}
}

// SIGTERM para a CLI: repassado ao cliente, credencial removida, CLI viva.
func TestRunMySQLWithSecretsForwardsSIGTERM(t *testing.T) {
	build, out := helperCommand(t, "wait-term")
	done := make(chan error, 1)
	go func() { done <- runMySQLWithSecrets(testMySQLPassword, build) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(out + ".ready"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper não ficou pronto")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "143") {
			t.Fatalf("err = %v, quero o exit 143 do cliente", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM não chegou ao cliente")
	}
	rep := readHelperReport(t, out)
	if _, err := os.Stat(filepath.Dir(optionsFileFromArgs(t, rep.Args))); !os.IsNotExist(err) {
		t.Fatalf("credencial sobrou após SIGTERM: %v", err)
	}
}

// Ctrl+C: o terminal entrega ao cliente; a CLI não morre nem repassa (o mysql
// cancelaria a query duas vezes) — espera o cliente e limpa.
func TestRunMySQLWithSecretsSurvivesSIGINT(t *testing.T) {
	build, out := helperCommand(t, "sleep")
	done := make(chan error, 1)
	go func() { done <- runMySQLWithSecrets(testMySQLPassword, build) }()
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cliente não terminou")
	}
	rep := readHelperReport(t, out)
	if _, err := os.Stat(filepath.Dir(optionsFileFromArgs(t, rep.Args))); !os.IsNotExist(err) {
		t.Fatalf("credencial sobrou: %v", err)
	}
}

// Postgres: argv de pg_dump/pg_restore/psql idêntico ao de antes.
func TestPostgresToolsArgvUnchanged(t *testing.T) {
	const conn = "postgresql://app:pw@abc123xy.db.upuai.cloud:5432/app?sslmode=verify-full&sslrootcert=system"

	t.Run("backup", func(t *testing.T) {
		newPublicAccessFake(t, func(method string, n int, body string) (int, string) { return 200, pgLegacyJSON })
		record := installFakeDBTools(t, "pg_dump")
		t.Setenv("FAKE_DUMP", "PGDMP")
		dbBackupOut = "x.dump"
		var err error
		captureStdout(t, func() { err = dbBackupCmd.RunE(dbBackupCmd, nil) })
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"--format=custom", "--no-owner", "--no-acl", conn}
		if got := recordedArgs(t, record); !slices.Equal(got, want) {
			t.Fatalf("pg_dump argv = %q, quero %q", got, want)
		}
		if data, _ := os.ReadFile("x.dump"); string(data) != "PGDMP" {
			t.Fatalf("dump = %q", data)
		}
	})

	t.Run("restore", func(t *testing.T) {
		newPublicAccessFake(t, func(method string, n int, body string) (int, string) { return 200, pgLegacyJSON })
		record := installFakeDBTools(t, "pg_restore")
		if err := os.WriteFile("x.dump", []byte("PGDMP"), 0o600); err != nil {
			t.Fatal(err)
		}
		dbRestoreIn, flagYes = "x.dump", true
		var err error
		captureStdout(t, func() { err = dbRestoreCmd.RunE(dbRestoreCmd, nil) })
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"--no-owner", "--no-acl", "--clean", "--if-exists", "-d", conn, "x.dump"}
		if got := recordedArgs(t, record); !slices.Equal(got, want) {
			t.Fatalf("pg_restore argv = %q, quero %q", got, want)
		}
	})

	t.Run("connect", func(t *testing.T) {
		newPublicAccessFake(t, func(method string, n int, body string) (int, string) { return 200, pgLegacyJSON })
		record := installFakeDBTools(t, "psql")
		var err error
		captureStdout(t, func() { err = dbConnectCmd.RunE(dbConnectCmd, nil) })
		if err != nil {
			t.Fatal(err)
		}
		if got := recordedArgs(t, record); !slices.Equal(got, []string{conn}) {
			t.Fatalf("psql argv = %q", got)
		}
	})

	// Postgres desligado com --enable: um GET e um PUT com allowlist vazia,
	// como antes.
	t.Run("auto-enable", func(t *testing.T) {
		reqs := newPublicAccessFake(t, func(method string, n int, body string) (int, string) {
			if method == http.MethodPut {
				return 200, pgLegacyJSON
			}
			return 200, pgDisabledLegacyJSON
		})
		installFakeDBTools(t, "psql")
		dbAutoEnable = true
		var err error
		out := captureStdout(t, func() { err = dbConnectCmd.RunE(dbConnectCmd, nil) })
		if err != nil {
			t.Fatal(err)
		}
		if len(*reqs) != 2 || (*reqs)[1].Method != http.MethodPut || (*reqs)[1].Body != `{"enabled":true,"allowedCidrs":[]}` {
			t.Fatalf("requests = %+v", *reqs)
		}
		for _, want := range []string{"public access enabled at abc123xy.db.upuai.cloud:5432", "open to any IP"} {
			if !strings.Contains(out, want) {
				t.Fatalf("saída sem %q: %q", want, out)
			}
		}
	})
}
