package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/cabundle"
	"github.com/upuai-cloud/cli/internal/ui"
)

// `upuai db connect|backup|restore` num MySQL gerenciado. O MySQL fala primeiro
// e o cliente não manda SNI, então cada banco tem a PRÓPRIA porta pública
// (<slug>.db.upuai.cloud:<23306–23505>); o TLS termina no mysqld com o wildcard
// Let's Encrypt e o cliente verifica a identidade do servidor.
// Runbook: upuai-core/docs/runbooks/2026-10-07-mysql-public-endpoint.md
//
// Credencial: a senha NUNCA vai em argv (visível em `ps` para qualquer usuário
// da máquina) nem em env (MYSQL_PWD é obsoleto e herdado por subprocessos). Ela
// vai num arquivo de opções 0600 dentro de um diretório privado (0700), passado
// como PRIMEIRO argumento — o cliente MySQL só aceita --defaults-file /
// --defaults-extra-file nessa posição — e removido quando o cliente termina,
// inclusive em erro e em SIGINT/SIGTERM/SIGHUP.

// mysqlOptionsFileFlag: --defaults-file, e não --defaults-extra-file. Com o
// extra-file o cliente ainda lê ~/.my.cnf DEPOIS do nosso arquivo, e um
// `password=` de [client]/[mysql] ali (comum em quem tem MySQL local) vence a
// senha da plataforma — o login falha com "Access denied" sem motivo aparente.
// Com --defaults-file só o nosso arquivo é lido (o Oracle ainda lê
// ~/.mylogin.cnf). Efeito colateral aceito: prompt/pager do ~/.my.cnf não valem
// na sessão do `db connect`.
const mysqlOptionsFileFlag = "--defaults-file"

// mysqlDumpFlags: dump consistente (InnoDB, sem lock), com rotinas, eventos e
// triggers. O servidor roda GTID ON (MOCO); --set-gtid-purged=OFF evita o
// SET @@GLOBAL.GTID_PURGED que só restaura num servidor vazio e exige
// privilégio de admin. --no-tablespaces dispensa o privilégio PROCESS que a
// conta de aplicação não tem.
var mysqlDumpFlags = []string{
	"--single-transaction",
	"--routines",
	"--events",
	"--triggers",
	"--set-gtid-purged=OFF",
	"--no-tablespaces",
}

// Ferramentas procuradas no PATH, na ordem. Os nomes mariadb* cobrem distros
// que só instalam os binários novos do MariaDB.
var (
	mysqlClientNames = []string{"mysql", "mariadb"}
	mysqlDumpNames   = []string{"mysqldump", "mariadb-dump"}
)

// Espera do restart único do MySQL (serverTlsReady) e do roteamento da porta.
var (
	mysqlTLSPollInterval      = 3 * time.Second
	mysqlTLSWaitTimeout       = 3 * time.Minute
	mysqlRouteProbeTimeout    = 15 * time.Second
	mysqlRouteProbeInterval   = 1 * time.Second
	mysqlRouteProbeReadWindow = 3 * time.Second
)

const mysqlTLSWaitMessage = "Restarting the database once to load its TLS certificate (about 1 minute)..."

// mysqlFlavor separa o cliente da Oracle do MariaDB: Debian/Ubuntu instalam o
// cliente do MariaDB como `mysql`, e ele não conhece --ssl-mode nem
// --set-gtid-purged.
type mysqlFlavor int

const (
	mysqlFlavorOracle mysqlFlavor = iota
	mysqlFlavorMariaDB
)

func (f mysqlFlavor) String() string {
	if f == mysqlFlavorMariaDB {
		return "MariaDB"
	}
	return "MySQL"
}

// detectMySQLFlavor lê a saída de `<tool> --version`.
func detectMySQLFlavor(versionOutput string) mysqlFlavor {
	if strings.Contains(strings.ToLower(versionOutput), "mariadb") {
		return mysqlFlavorMariaDB
	}
	return mysqlFlavorOracle
}

type mysqlTool struct {
	Name   string
	Path   string
	Flavor mysqlFlavor
}

// Injetáveis para teste.
var (
	mysqlLookPath    = exec.LookPath
	mysqlToolVersion = func(path string) (string, error) {
		out, err := exec.Command(path, "--version").CombinedOutput()
		return string(out), err
	}
)

// findMySQLTool acha o primeiro binário de names no PATH e detecta o sabor.
func findMySQLTool(names []string) (mysqlTool, error) {
	for _, name := range names {
		path, err := mysqlLookPath(name)
		if err != nil {
			continue
		}
		flavor := mysqlFlavorOracle
		if strings.HasPrefix(name, "mariadb") {
			flavor = mysqlFlavorMariaDB
		}
		if out, verr := mysqlToolVersion(path); verr == nil {
			if detectMySQLFlavor(out) == mysqlFlavorMariaDB {
				flavor = mysqlFlavorMariaDB
			}
		}
		return mysqlTool{Name: name, Path: path, Flavor: flavor}, nil
	}
	return mysqlTool{}, mysqlToolMissingError(names[0])
}

// mysqlToolMissingError espelha o "psql not found — install ..." do Postgres,
// com o caminho de instalação de cada sistema.
func mysqlToolMissingError(tool string) error {
	return fmt.Errorf(`%s not found — install the MySQL client:
  macOS:          brew install mysql-client, then add "$(brew --prefix mysql-client)/bin" to PATH (keg-only)
  Debian/Ubuntu:  sudo apt install default-mysql-client (or mysql-client)
  Windows:        MySQL Installer (https://dev.mysql.com/downloads/installer/), then add the server's bin folder to PATH
or get the connection details for another client with: upuai db connect --print`, tool)
}

// mysqlTarget são os campos de conexão vindos da API.
type mysqlTarget struct {
	Host     string
	Port     int
	User     string
	Database string
}

func mysqlTargetFrom(info *api.PublicAccessInfo) (mysqlTarget, error) {
	t := mysqlTarget{Host: info.Host, Port: info.Port, User: info.Username, Database: info.Database}
	if t.Host == "" || t.Port <= 0 || t.User == "" || info.Password == "" {
		return t, fmt.Errorf("the API did not return the MySQL connection details (host, port, user, password) — check 'upuai db public status' and retry")
	}
	// O nome do banco vai posicional no mysqldump: um "-" inicial viraria flag.
	if strings.HasPrefix(t.Database, "-") {
		return t, fmt.Errorf("unexpected database name %q", t.Database)
	}
	return t, nil
}

// mysqlTLSArgs: verificação completa da identidade (cadeia + hostname).
func mysqlTLSArgs(flavor mysqlFlavor, caFile string) []string {
	if flavor == mysqlFlavorMariaDB {
		return []string{"--ssl", "--ssl-verify-server-cert", "--ssl-ca=" + caFile}
	}
	return []string{"--ssl-mode=VERIFY_IDENTITY", "--ssl-ca=" + caFile}
}

// mysqlBaseArgs: arquivo de opções PRIMEIRO, depois destino e TLS.
func mysqlBaseArgs(flavor mysqlFlavor, optionsFile, caFile string, t mysqlTarget) []string {
	args := []string{
		mysqlOptionsFileFlag + "=" + optionsFile,
		"--host=" + t.Host,
		"--port=" + strconv.Itoa(t.Port),
		"--user=" + t.User,
	}
	return append(args, mysqlTLSArgs(flavor, caFile)...)
}

// mysqlClientArgs: `mysql` interativo (connect) e em lote (restore).
func mysqlClientArgs(flavor mysqlFlavor, optionsFile, caFile string, t mysqlTarget) []string {
	args := mysqlBaseArgs(flavor, optionsFile, caFile, t)
	if t.Database != "" {
		args = append(args, "--database="+t.Database)
	}
	return args
}

// mysqlDumpArgs: `mysqldump` do banco da connection string (posicional, sem
// --databases: o dump não fixa o nome e restaura em qualquer banco).
func mysqlDumpArgs(flavor mysqlFlavor, optionsFile, caFile string, t mysqlTarget) []string {
	args := mysqlBaseArgs(flavor, optionsFile, caFile, t)
	for _, f := range mysqlDumpFlags {
		if flavor == mysqlFlavorMariaDB && strings.HasPrefix(f, "--set-gtid-purged") {
			continue
		}
		args = append(args, f)
	}
	if t.Database != "" {
		args = append(args, t.Database)
	}
	return args
}

// mysqlOptionsFileContent: grupo [client] (lido por mysql, mysqldump e pelos
// clientes MariaDB). Valor entre aspas com os escapes do parser de option
// files — a senha gerada pela plataforma é alfanumérica, mas o arquivo não pode
// quebrar se um dia não for.
func mysqlOptionsFileContent(password string) string {
	return "[client]\npassword=\"" + escapeMySQLOptionValue(password) + "\"\n"
}

var mysqlOptionEscaper = strings.NewReplacer(
	`\`, `\\`,
	`"`, `\"`,
	"\n", `\n`,
	"\r", `\r`,
	"\t", `\t`,
	"\b", `\b`,
)

func escapeMySQLOptionValue(s string) string {
	return mysqlOptionEscaper.Replace(s)
}

// mysqlSecrets é o diretório privado com o arquivo de opções e, quando o
// sistema não tem bundle de CAs, as raízes embutidas.
type mysqlSecrets struct {
	Dir         string
	OptionsFile string
	CAFile      string
	CASource    cabundle.Source
}

func newMySQLSecrets(password string) (*mysqlSecrets, error) {
	dir, err := os.MkdirTemp("", "upuai-mysql-")
	if err != nil {
		return nil, fmt.Errorf("create temporary directory: %w", err)
	}
	s := &mysqlSecrets{Dir: dir}
	s.OptionsFile = filepath.Join(dir, "client.cnf")
	if err := writePrivateFile(s.OptionsFile, []byte(mysqlOptionsFileContent(password))); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("write MySQL option file: %w", err)
	}
	bundle, err := cabundle.Resolve(dir)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	s.CAFile, s.CASource = bundle.Path, bundle.Source
	return s, nil
}

// Close apaga o diretório inteiro (senha e bundle embutido).
func (s *mysqlSecrets) Close() error {
	return os.RemoveAll(s.Dir)
}

// writePrivateFile cria o arquivo 0600 (O_EXCL: nunca segue um arquivo
// plantado). O Chmod explícito cobre umask exóticos.
func writePrivateFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// runMySQLWithSecrets grava a credencial, roda o cliente montado por build e
// apaga tudo quando o processo termina. Sinais: o Ctrl+C do terminal chega ao
// grupo de processos inteiro — o cliente o trata (o mysql cancela a query) e
// este processo só espera; SIGTERM/SIGHUP enviados a este processo são
// repassados ao cliente. Em nenhum caso a CLI morre antes de limpar.
func runMySQLWithSecrets(password string, build func(s *mysqlSecrets) *exec.Cmd) (err error) {
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)

	secrets, err := newMySQLSecrets(password)
	if err != nil {
		return err
	}
	defer func() {
		if rerr := secrets.Close(); rerr != nil && err == nil {
			err = fmt.Errorf("remove the temporary MySQL credentials in %s: %w", secrets.Dir, rerr)
		}
	}()

	// Sinal durante o preparo: não chega a abrir conexão.
	select {
	case sig := <-sigs:
		return fmt.Errorf("interrupted (%s)", sig)
	default:
	}

	c := build(secrets)
	if err := c.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return mysqlToolMissingError(filepath.Base(c.Path))
		}
		return fmt.Errorf("start %s: %w", filepath.Base(c.Path), err)
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigs:
				if sig != os.Interrupt {
					_ = c.Process.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()
	err = c.Wait()
	close(done)
	return err
}

// ─── connect / backup / restore ──────────────────────────────────────────────

// runMySQLConnect abre o mysql interativo. Devolve o exit code do cliente para
// o chamador sair com ele DEPOIS da limpeza (os.Exit pula defers).
func runMySQLConnect(sess *publicAccessSession, tool mysqlTool) (int, error) {
	info := sess.info
	target, err := mysqlTargetFrom(info)
	if err != nil {
		return 0, err
	}
	sess.waitForMySQLRoute()
	ui.PrintInfo(fmt.Sprintf("opening %s → %s:%d", tool.Name, info.Host, info.Port))
	err = runMySQLWithSecrets(info.Password, func(s *mysqlSecrets) *exec.Cmd {
		c := exec.Command(tool.Path, mysqlClientArgs(tool.Flavor, s.OptionsFile, s.CAFile, target)...)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		return c
	})
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// Interativo: sair com erro (query falha antes do \q, Ctrl+D numa
		// transação) é esperado — o código vai adiante como no psql.
		return exitErr.ExitCode(), nil
	}
	return 0, err
}

// mysqlBackupFileName: <service>-<UTC AAAAMMDD-HHMMSS>.sql.
func mysqlBackupFileName(service string, now time.Time) string {
	var b strings.Builder
	for _, r := range service {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-.")
	if name == "" {
		name = "mysql"
	}
	return fmt.Sprintf("%s-%s.sql", name, now.UTC().Format("20060102-150405"))
}

// mysqlBackupServiceName: slug (ou nome) do banco para o arquivo padrão.
func mysqlBackupServiceName(sess *publicAccessSession) string {
	svc, err := findServiceByID(sess.client, sess.serviceID)
	if err != nil || svc == nil {
		return ""
	}
	if svc.Slug != "" {
		return svc.Slug
	}
	return svc.Name
}

func runMySQLBackup(sess *publicAccessSession) error {
	// Ferramenta antes de publicar o banco: sem mysqldump não há o que abrir.
	tool, err := findMySQLTool(mysqlDumpNames)
	if err != nil {
		return err
	}
	outPath := dbBackupOut
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if outPath == "" {
		outPath = mysqlBackupFileName(mysqlBackupServiceName(sess), time.Now())
		// Nome gerado: nunca sobrescreve um arquivo existente.
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	if err := sess.ensureEnabled(); err != nil {
		return err
	}
	info := sess.info
	target, err := mysqlTargetFrom(info)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(outPath, flags, 0o600)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	sess.waitForMySQLRoute()
	ui.PrintInfo(fmt.Sprintf("running %s → %s", tool.Name, outPath))
	stderr := newTailBuffer(16 << 10)
	runErr := runMySQLWithSecrets(info.Password, func(s *mysqlSecrets) *exec.Cmd {
		c := exec.Command(tool.Path, mysqlDumpArgs(tool.Flavor, s.OptionsFile, s.CAFile, target)...)
		c.Stdout = out
		c.Stderr = io.MultiWriter(os.Stderr, stderr)
		return c
	})
	closeErr := out.Close()
	if runErr == nil && closeErr != nil {
		runErr = fmt.Errorf("write %s: %w", outPath, closeErr)
	}
	if runErr != nil {
		// Um dump truncado parece válido até o restore: não deixa para trás.
		_ = os.Remove(outPath)
		return explainMySQLToolError(tool, runErr, stderr.String(), info)
	}
	if fi, statErr := os.Stat(outPath); statErr == nil {
		ui.PrintSuccess(fmt.Sprintf("backup written: %s (%d bytes)", outPath, fi.Size()))
	} else {
		ui.PrintSuccess(fmt.Sprintf("backup written: %s", outPath))
	}
	return nil
}

func runMySQLRestore(sess *publicAccessSession, inPath string) error {
	// Ferramenta antes de publicar o banco, como no backup.
	tool, err := findMySQLTool(mysqlClientNames)
	if err != nil {
		return err
	}
	if err := sess.ensureEnabled(); err != nil {
		return err
	}
	info := sess.info
	target, err := mysqlTargetFrom(info)
	if err != nil {
		return err
	}
	in, err := os.Open(inPath)
	if err != nil {
		return fmt.Errorf("input file: %w", err)
	}
	defer func() { _ = in.Close() }()

	sess.waitForMySQLRoute()
	ui.PrintInfo(fmt.Sprintf("running %s < %s", tool.Name, inPath))
	stderr := newTailBuffer(16 << 10)
	runErr := runMySQLWithSecrets(info.Password, func(s *mysqlSecrets) *exec.Cmd {
		c := exec.Command(tool.Path, mysqlClientArgs(tool.Flavor, s.OptionsFile, s.CAFile, target)...)
		// Stream: o arquivo nunca é carregado em memória.
		c.Stdin = mysqlRestoreReader(in, tool.Flavor)
		c.Stdout = os.Stdout
		c.Stderr = io.MultiWriter(os.Stderr, stderr)
		return c
	})
	if runErr != nil {
		return explainMySQLToolError(tool, runErr, stderr.String(), info)
	}
	ui.PrintSuccess("restore complete")
	return nil
}

// mariadbSandboxLine é a primeira linha dos dumps do mariadb-dump/mysqldump do
// MariaDB desde 10.5.25/10.6.18/10.11.8/11.4.2 (o cliente padrão do
// Debian/Ubuntu). O cliente da Oracle não conhece o comando `\-` e aborta na
// linha 1 — então ela é descartada no stream quando o restore usa o cliente da
// Oracle. Para o cliente MariaDB ela fica (ativa o sandbox dele).
const mariadbSandboxLine = `/*M!999999\- enable the sandbox mode */`

func mysqlRestoreReader(r io.Reader, flavor mysqlFlavor) io.Reader {
	br := bufio.NewReaderSize(r, 64<<10)
	if flavor == mysqlFlavorOracle {
		if head, _ := br.Peek(len(mariadbSandboxLine)); string(head) == mariadbSandboxLine {
			_, _ = br.ReadString('\n')
		}
	}
	return br
}

// ─── formato do dump ─────────────────────────────────────────────────────────

type dumpFormat int

const (
	dumpFormatUnknown        dumpFormat = iota
	dumpFormatPostgresCustom            // pg_dump --format=custom (magic PGDMP)
	dumpFormatPostgresTar               // pg_dump --format=tar (toc.dat)
	dumpFormatPostgresSQL               // pg_dump plain
	dumpFormatPostgresDir               // pg_dump --format=directory
	dumpFormatMySQLSQL                  // mysqldump / mariadb-dump
	dumpFormatGzip                      // .gz
)

// sniffDumpFormat olha só o começo do arquivo (barato, sem carregar o dump).
func sniffDumpFormat(head []byte) dumpFormat {
	switch {
	case bytes.HasPrefix(head, []byte("PGDMP")):
		return dumpFormatPostgresCustom
	case bytes.HasPrefix(head, []byte{0x1f, 0x8b}):
		return dumpFormatGzip
	case len(head) >= 262 && string(head[257:262]) == "ustar" && string(bytes.TrimRight(head[:100], "\x00")) == "toc.dat":
		return dumpFormatPostgresTar
	case bytes.HasPrefix(head, []byte(mariadbSandboxLine)),
		bytes.Contains(head, []byte("-- MySQL dump")),
		bytes.Contains(head, []byte("-- MariaDB dump")):
		return dumpFormatMySQLSQL
	case bytes.Contains(head, []byte("-- PostgreSQL database dump")):
		return dumpFormatPostgresSQL
	}
	return dumpFormatUnknown
}

func detectDumpFormat(path string) (dumpFormat, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return dumpFormatUnknown, err
	}
	if fi.IsDir() {
		if _, err := os.Stat(filepath.Join(path, "toc.dat")); err == nil {
			return dumpFormatPostgresDir, nil
		}
		return dumpFormatUnknown, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return dumpFormatUnknown, err
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 4096)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return dumpFormatUnknown, err
	}
	return sniffDumpFormat(head[:n]), nil
}

// checkDumpMatchesEngine recusa um dump do engine errado antes de qualquer
// mudança. Só os formatos reconhecíveis — o resto segue para a ferramenta.
// Postgres mantém o comportamento anterior para tudo que não é mysqldump.
func checkDumpMatchesEngine(path string, info *api.PublicAccessInfo) error {
	format, err := detectDumpFormat(path)
	if err != nil {
		return fmt.Errorf("input file: %w", err)
	}
	if info.IsMySQL() {
		switch format {
		case dumpFormatPostgresCustom, dumpFormatPostgresTar, dumpFormatPostgresSQL, dumpFormatPostgresDir:
			return fmt.Errorf("%s is a PostgreSQL dump (pg_dump) and this database is MySQL — restore it into a PostgreSQL database, or restore a mysqldump .sql file here", path)
		case dumpFormatGzip:
			return fmt.Errorf("%s is gzip-compressed — decompress it first (gunzip -k %s) and restore the .sql file", path, path)
		}
		if fi, statErr := os.Stat(path); statErr == nil && fi.IsDir() {
			return fmt.Errorf("%s is a directory — a MySQL restore takes a .sql file (from mysqldump or 'upuai db backup')", path)
		}
		return nil
	}
	if format == dumpFormatMySQLSQL {
		return fmt.Errorf("%s is a MySQL dump (mysqldump) and this database is PostgreSQL — restore it into a MySQL database", path)
	}
	return nil
}

// ─── espera do certificado e do roteamento ───────────────────────────────────

// waitForServerTLS consulta o endpoint até serverTlsReady (o restart único do
// MySQL terminou). Durante o restart a API pode responder que o banco não está
// pronto, ou o gateway falhar — isso é esperar mais, não erro.
func waitForServerTLS(poll func() (*api.PublicAccessInfo, error), interval, timeout time.Duration) (*api.PublicAccessInfo, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		info, err := poll()
		switch {
		case err == nil && !info.Enabled:
			return nil, fmt.Errorf("public access was disabled while the database was restarting — enable it again with: %s", dbCommandHint("public enable"))
		case err == nil && info.ServerTLSReady:
			return info, nil
		case err == nil:
			lastErr = nil
		case retryableWhileRestarting(err):
			lastErr = err
		default:
			return nil, err
		}
		if !time.Now().Add(interval).Before(deadline) {
			msg := fmt.Sprintf("the database is still restarting to load its TLS certificate after %s — public access is enabled; check again with '%s' and retry", timeout, dbCommandHint("public status"))
			if lastErr != nil {
				msg += fmt.Sprintf(" (last error: %v)", lastErr)
			}
			return nil, errors.New(msg)
		}
		time.Sleep(interval)
	}
}

func retryableWhileRestarting(err error) bool {
	switch api.ErrorCode(err) {
	case "DB_NOT_READY", "DB_OPERATION_IN_PROGRESS":
		return true
	}
	switch api.StatusCode(err) {
	case 0, // rede
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// waitForMySQLRoute: logo após publicar (ou após o restart), o Traefik leva
// ~2s para rotear a porta nova — antes disso a conexão fecha sem resposta e o
// cliente falharia com "Lost connection ... reading initial communication
// packet". O MySQL fala primeiro, então a saudação do servidor prova a rota
// sem autenticar nada. Sem resposta na janela, segue assim mesmo: o cliente
// reporta o erro real.
func (s *publicAccessSession) waitForMySQLRoute() {
	if !s.routeMayLag {
		return
	}
	addr := net.JoinHostPort(s.info.Host, strconv.Itoa(s.info.Port))
	_ = ui.RunWithSpinner(fmt.Sprintf("Waiting for %s to answer...", addr), func() error {
		if mysqlRouteProbe(addr, mysqlRouteProbeTimeout, mysqlRouteProbeInterval) {
			return nil
		}
		return fmt.Errorf("no answer from %s yet", addr)
	})
	s.routeMayLag = false
}

// mysqlRouteProbe é injetável: os testes nunca discam um host real.
var mysqlRouteProbe = waitForMySQLGreeting

func waitForMySQLGreeting(addr string, timeout, interval time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if mysqlGreetingReceived(addr, mysqlRouteProbeReadWindow) {
			return true
		}
		if !time.Now().Add(interval).Before(deadline) {
			return false
		}
		time.Sleep(interval)
	}
}

// mysqlGreetingReceived: o primeiro pacote do servidor tem sequência 0 e é o
// handshake v10 (0x0a) ou um pacote de erro (0xff, ex. too many connections) —
// ambos provam que a porta chega ao mysqld.
func mysqlGreetingReceived(addr string, window time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, window)
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(window))
	hdr := make([]byte, 5)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return false
	}
	return hdr[3] == 0 && (hdr[4] == 0x0a || hdr[4] == 0xff)
}

// ─── erros das ferramentas ───────────────────────────────────────────────────

// explainMySQLToolError traduz as falhas recorrentes (stderr do cliente) em
// orientação; o stderr bruto já foi espelhado no terminal.
func explainMySQLToolError(tool mysqlTool, err error, stderr string, info *api.PublicAccessInfo) error {
	addr := fmt.Sprintf("%s:%d", info.Host, info.Port)
	switch {
	case strings.Contains(stderr, "caching_sha2_password"):
		return fmt.Errorf("%s failed: this %s client cannot authenticate with MySQL 8 (caching_sha2_password plugin missing) — install the MySQL client (macOS: brew install mysql-client; Debian/Ubuntu: apt install mysql-client)", tool.Name, tool.Flavor)
	case strings.Contains(stderr, "unknown variable 'ssl-mode"),
		strings.Contains(stderr, "unknown option '--ssl-mode"),
		strings.Contains(stderr, "unknown variable 'set-gtid-purged"),
		strings.Contains(stderr, "unknown option '--set-gtid-purged"):
		return fmt.Errorf("%s failed: the client rejected a flag for its detected flavor (%s) — check '%s --version' and report it", tool.Name, tool.Flavor, tool.Path)
	case strings.Contains(stderr, "ERROR 2026"),
		strings.Contains(stderr, "SSL connection error"),
		strings.Contains(stderr, "TLS/SSL error"),
		strings.Contains(stderr, "certificate verify failed"):
		return fmt.Errorf("%s failed: TLS verification of %s failed — set SSL_CERT_FILE to a CA bundle that trusts ISRG Root X1, or update the client", tool.Name, addr)
	case strings.Contains(stderr, "Access denied for user"):
		return fmt.Errorf("%s failed: the database refused the credentials — if the password was changed outside the platform, run '%s'", tool.Name, dbCommandHint("credentials repair"))
	case strings.Contains(stderr, "ERROR 1227"), strings.Contains(stderr, "Access denied; you need"):
		return fmt.Errorf("%s failed: the dump has statements that need administrative privileges (SET @@GLOBAL.GTID_PURGED / SQL_LOG_BIN from a dump taken with GTIDs, or a DEFINER for another account) — re-create it with mysqldump --set-gtid-purged=OFF, as 'upuai db backup' does", tool.Name)
	case strings.Contains(stderr, "ERROR 2003"),
		strings.Contains(stderr, "ERROR 2013"),
		strings.Contains(stderr, "Can't connect to"),
		strings.Contains(stderr, "Lost connection to"):
		return fmt.Errorf("%s failed: could not keep a connection to %s — check that your IP may connect ('%s'); right after enabling public access the route takes a few seconds, retry", tool.Name, addr, dbCommandHint("public status"))
	}
	return fmt.Errorf("%s failed: %w", tool.Name, err)
}

// tailBuffer guarda só os últimos max bytes do stderr (um restore grande pode
// escrever muito; o diagnóstico está no fim).
type tailBuffer struct {
	max int
	buf []byte
}

func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.buf) }
