package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
	"golang.org/x/term"
)

// `upuai db ...` wraps the Public DB Endpoint feature so customers can
// connect / dump / restore from anywhere with a one-liner.
// Runbook: upuai-core/docs/runbooks/2026-04-24-public-db-endpoint.md

var (
	dbBackupOut    string
	dbRestoreIn    string
	dbAutoEnable   bool
	dbConnectPrint bool
	// dbServiceRef permite override explícito do banco-alvo (paridade com -s/--service
	// dos outros comandos). Vazio = resolve o único service tipo=database do projeto.
	dbServiceRef string
	// dbAllowCIDRs / dbAllowAny: quem pode conectar no endpoint público.
	// --allow é repetível; --any abre para qualquer IP (explícito de propósito:
	// sem flag nenhuma, `db public enable` PRESERVA a allowlist existente).
	dbAllowCIDRs []string
	dbAllowAny   bool
)

// databaseServiceType é o discriminador que a API usa para identificar o
// Service.type de bancos gerenciados (CNPG/MySQL/Mongo/Redis vão pelo orchestrator
// /databases). Os outros tipos (github/docker/bucket) NÃO têm CNPG cluster e
// portanto NÃO podem expor public-access — bater no endpoint com eles vira 500.
const databaseServiceType = "database"

var dbCmd = &cobra.Command{
	Use:     "db",
	Aliases: []string{"database"},
	Short:   "Manage the linked database (connect, backup, restore, extensions, updates)",
	Long: `Manage the linked database service.

connect, backup and restore work on managed PostgreSQL (psql, pg_dump,
pg_restore) and MySQL (mysql, mysqldump) through the public endpoint, with TLS
and full server identity verification.

Examples:
  upuai db connect                  Open an interactive psql / mysql session
  upuai db connect --print          Print the public connection string and exit
  upuai db backup --out file.dump   Run pg_dump (PostgreSQL) or mysqldump (MySQL)
  upuai db restore -f file.dump     Restore via pg_restore (PostgreSQL) or mysql (MySQL)
  upuai db public                   Show the public endpoint and who may connect
  upuai db public enable --allow IP Publish restricted to one origin
  upuai db public disable           Remove the public endpoint
  upuai db extensions               List managed Postgres extensions (PostGIS, pgvector, ...)
  upuai db extensions enable postgis  Enable an extension (instant, no restart)
  upuai db version                  Show the running version and pending updates
  upuai db update --wait            Apply the pending maintenance update (restarts the database)`,
}

var dbConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Open an interactive psql (PostgreSQL) or mysql (MySQL) session against the linked database",
	Long: `Open an interactive shell against the linked database via the public endpoint.

PostgreSQL: requires psql (libpq / postgresql-client).
MySQL: requires the mysql client (macOS: brew install mysql-client; Debian/Ubuntu:
apt install default-mysql-client). TLS is verified against the server identity
(--ssl-mode=VERIFY_IDENTITY); the password goes in a private temporary option
file (0600, removed when the client exits), never on the command line.

Use --print to skip the client and just emit the connection string
(script-friendly), or --output json to emit the full access info object
(host, port, username, password, database).

If public access is currently disabled, you'll be prompted to enable it (use
--yes or --enable to skip the prompt). A MySQL created before public access
restarts once (about 1 minute) the first time it is enabled — that needs --yes
or an interactive confirmation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sess, err := fetchPublicAccess()
		if err != nil {
			return err
		}
		format := getOutputFormat()
		// MySQL interativo: o cliente local é pré-requisito — sem ele não há por
		// que publicar (nem reiniciar) o banco.
		var mysqlClient *mysqlTool
		if sess.info.IsMySQL() && format != ui.FormatJSON && !dbConnectPrint {
			tool, err := findMySQLTool(mysqlClientNames)
			if err != nil {
				return err
			}
			mysqlClient = &tool
		}
		if err := sess.ensureEnabled(); err != nil {
			return err
		}
		info := sess.info

		if format == ui.FormatJSON {
			ui.PrintJSON(info)
			return nil
		}
		if dbConnectPrint {
			if info.IsMySQL() {
				ui.PrintKeyValue("Host", info.Host, "Port", fmt.Sprintf("%d", info.Port), "User", info.Username, "Database", info.Database)
			} else {
				ui.PrintKeyValue("Host", info.Host, "Port", fmt.Sprintf("%d", info.Port))
			}
			fmt.Println()
			fmt.Println(info.ConnectionString)
			return nil
		}

		if mysqlClient != nil {
			code, err := runMySQLConnect(sess, *mysqlClient)
			if err != nil {
				return err
			}
			// Mesmo contrato do psql: o exit code do cliente interativo é o do
			// comando. Os arquivos temporários já foram removidos aqui.
			if code != 0 {
				os.Exit(code)
			}
			return nil
		}

		ui.PrintInfo(fmt.Sprintf("opening psql → %s:%d", info.Host, info.Port))
		c := exec.Command("psql", withSystemTrustStore(info.ConnectionString))
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		// psql é interativo — exit codes não-zero (ex: usuário sair com Ctrl+D no
		// meio de uma transação) são esperados. Não envolvemos no runLibpqTool
		// porque queremos propagar o exit code real, não o "psql failed".
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			if errors.Is(err, exec.ErrNotFound) {
				return fmt.Errorf("psql not found — install postgresql-client (macOS: brew install postgresql)")
			}
			return fmt.Errorf("psql failed: %w", err)
		}
		return nil
	},
}

var dbBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Dump the linked database: pg_dump (PostgreSQL) or mysqldump (MySQL)",
	Long: `Dump the linked database through its public endpoint.

PostgreSQL: wraps pg_dump (custom format, --no-owner --no-acl); requires
pg_dump (libpq / postgresql-client) and --out.

MySQL: wraps mysqldump with --single-transaction --routines --events --triggers
--set-gtid-purged=OFF --no-tablespaces (a consistent dump restorable into any
MySQL server; MariaDB's mysqldump has no --set-gtid-purged and skips it), with
TLS identity verification. Requires mysqldump (the mysql client package).
--out defaults to <service>-<UTC timestamp>.sql in the current directory; an
incomplete file is removed if the dump fails.

If public access is disabled, you'll be asked to enable it first.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sess, err := fetchPublicAccess()
		if err != nil {
			return err
		}
		if sess.info.IsMySQL() {
			return runMySQLBackup(sess)
		}
		if dbBackupOut == "" {
			return fmt.Errorf("--out is required (path to .dump file)")
		}
		if err := sess.ensureEnabled(); err != nil {
			return err
		}
		info := sess.info
		out, err := os.Create(dbBackupOut)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer func() { _ = out.Close() }()

		ui.PrintInfo(fmt.Sprintf("running pg_dump → %s", dbBackupOut))
		c := exec.Command("pg_dump", "--format=custom", "--no-owner", "--no-acl", withSystemTrustStore(info.ConnectionString))
		c.Stdout = out
		if err := runLibpqTool("pg_dump", c); err != nil {
			return err
		}
		if fi, statErr := os.Stat(dbBackupOut); statErr == nil {
			ui.PrintSuccess(fmt.Sprintf("backup written: %s (%d bytes)", dbBackupOut, fi.Size()))
		} else {
			ui.PrintSuccess(fmt.Sprintf("backup written: %s", dbBackupOut))
		}
		return nil
	},
}

var dbRestoreCmd = &cobra.Command{
	Use:   "restore [file]",
	Short: "Restore a dump into the linked database: pg_restore (PostgreSQL) or mysql (MySQL)",
	Long: `Restore a dump into the linked database through its public endpoint. The file
is the argument or -f/--file.

PostgreSQL: wraps pg_restore with --no-owner --no-acl --clean --if-exists to
drop+recreate matching objects; requires pg_restore (libpq / postgresql-client).

MySQL: streams the .sql file into the mysql client (TLS identity verified),
into the database of the connection string. Requires the mysql client.

A PostgreSQL dump is refused on a MySQL database and a mysqldump file on a
PostgreSQL database. If public access is disabled, you'll be asked to enable
it first.

WARNING: the restore writes to the live database — make sure the file is what
you intend to restore.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			if dbRestoreIn != "" && dbRestoreIn != args[0] {
				return fmt.Errorf("pass the dump file once: as the argument or with --file, not both")
			}
			dbRestoreIn = args[0]
		}
		if dbRestoreIn == "" {
			return fmt.Errorf("--file is required (path to .dump file)")
		}
		if _, err := os.Stat(dbRestoreIn); err != nil {
			return fmt.Errorf("input file: %w", err)
		}
		if !flagYes {
			confirmed, err := ui.Confirm(fmt.Sprintf("Restore %s into the linked database? This rewrites matching objects.", dbRestoreIn))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintWarning("aborted")
				return nil
			}
		}
		sess, err := fetchPublicAccess()
		if err != nil {
			return err
		}
		// Antes de publicar o banco: um dump do engine errado nunca justifica
		// abrir o endpoint.
		if err := checkDumpMatchesEngine(dbRestoreIn, sess.info); err != nil {
			return err
		}
		if sess.info.IsMySQL() {
			return runMySQLRestore(sess, dbRestoreIn)
		}
		if err := sess.ensureEnabled(); err != nil {
			return err
		}
		info := sess.info

		ui.PrintInfo(fmt.Sprintf("running pg_restore from %s", dbRestoreIn))
		c := exec.Command(
			"pg_restore",
			"--no-owner",
			"--no-acl",
			"--clean",
			"--if-exists",
			"-d", withSystemTrustStore(info.ConnectionString),
			dbRestoreIn,
		)
		c.Stdout = os.Stdout
		if err := runLibpqTool("pg_restore", c); err != nil {
			return err
		}
		ui.PrintSuccess("restore complete")
		return nil
	},
}

// stdinIsTerminal reports whether stdin is an interactive terminal. Used to
// avoid interactive prompts (which open /dev/tty) in scripts/pipes.
//
// term.IsTerminal (ioctl) em vez do bit de char device: /dev/null também é
// char device, então `upuai db ... < /dev/null` (CI, agentes, cron) passava por
// "terminal", caía no prompt e morria com "could not open a new TTY" em vez do
// erro acionável. Mesma checagem do `ssh`.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// publicAccessSession é o banco-alvo dos comandos connect/backup/restore e o
// estado do endpoint público dele.
type publicAccessSession struct {
	client    *api.Client
	envID     string
	serviceID string
	info      *api.PublicAccessInfo
	// routeMayLag: esta invocação acabou de publicar o endpoint (ou esperou o
	// restart do MySQL). O Traefik leva ~2s para rotear um banco recém-
	// publicado — o primeiro connect do MySQL espera a saudação do servidor.
	routeMayLag bool
}

// fetchPublicAccess autentica, resolve o banco e lê o estado do endpoint — sem
// mudar nada. connect/backup/restore chamam ensureEnabled em seguida; separados
// para que decidam pelo engine (cliente local, nome do backup, formato do dump)
// ANTES de publicar o banco.
func fetchPublicAccess() (*publicAccessSession, error) {
	if err := requireAuth(); err != nil {
		return nil, err
	}
	client := api.NewClient()
	envID, serviceID, err := resolveDatabaseService(client, dbServiceRef)
	if err != nil {
		return nil, err
	}

	var info *api.PublicAccessInfo
	if err := ui.RunWithSpinner("Fetching access status...", func() error {
		var apiErr error
		info, apiErr = client.GetDatabasePublicAccess(envID, serviceID)
		return apiErr
	}); err != nil {
		return nil, explainPublicAccessError(err, "get public access", false)
	}
	return &publicAccessSession{client: client, envID: envID, serviceID: serviceID, info: info}, nil
}

// ensureEnabled oferece publicar o endpoint quando desligado e, no MySQL,
// espera o certificado carregar (serverTlsReady) antes de devolver.
func (s *publicAccessSession) ensureEnabled() error {
	if s.info.Enabled {
		if s.info.IsMySQL() && !s.info.ServerTLSReady {
			// Publicado há instantes (por esta ou outra sessão): o banco ainda
			// reinicia para carregar o certificado — conectar agora falharia.
			return s.waitForServerTLS()
		}
		return nil
	}

	// MySQL anterior ao endpoint público: ligar reinicia o banco uma vez. É
	// disruptivo, então --enable sozinho não basta — exige --yes ou confirmação.
	needsRestart := s.info.IsMySQL() && !s.info.ServerTLSReady
	switch {
	case needsRestart && !flagYes:
		if !stdinIsTerminal() {
			return fmt.Errorf("public access is disabled, and enabling it restarts this MySQL once (about 1 minute) to load its TLS certificate — re-run with --yes to confirm (non-interactive)")
		}
		confirmed, err := ui.Confirm("Public access is disabled. Enabling it restarts this MySQL once (about 1 minute of downtime) to load its TLS certificate. Enable it now?")
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("public access required — re-run with --enable or enable in the dashboard")
		}
	case !dbAutoEnable && !flagYes:
		// Without a TTY (e.g. `db connect --print` in a script/pipe, or any
		// non-interactive shell), ui.Confirm would try to open /dev/tty and fail
		// with "could not open a new TTY". Surface an actionable error instead.
		if !stdinIsTerminal() {
			return fmt.Errorf("public access is disabled — re-run with --enable (non-interactive), --yes, or enable it in the dashboard")
		}
		confirmed, err := ui.Confirm("Public access is disabled. Enable it now?")
		if err != nil {
			return err
		}
		if !confirmed {
			return fmt.Errorf("public access required — re-run with --enable or enable in the dashboard")
		}
	}

	var info *api.PublicAccessInfo
	if err := ui.RunWithSpinner("Enabling public access...", func() error {
		var apiErr error
		// Endpoint estava desligado: não existe allowlist a preservar (desligar
		// apaga o middleware). Abre e avisa como restringir.
		info, apiErr = s.client.SetDatabasePublicAccess(s.envID, s.serviceID, true, nil)
		return apiErr
	}); err != nil {
		return explainPublicAccessError(err, "enable public access", true)
	}
	s.info = info
	s.routeMayLag = true
	ui.PrintSuccess(fmt.Sprintf("public access enabled at %s:%d", info.Host, info.Port))
	ui.PrintInfo("open to any IP — restrict with: upuai db public enable --allow <cidr>")
	if info.IsMySQL() && !info.ServerTLSReady {
		return s.waitForServerTLS()
	}
	return nil
}

// waitForServerTLS acompanha o restart único do MySQL até o certificado
// carregar e troca s.info pelo estado pronto.
func (s *publicAccessSession) waitForServerTLS() error {
	var ready *api.PublicAccessInfo
	err := ui.RunWithSpinner(mysqlTLSWaitMessage, func() error {
		var werr error
		ready, werr = waitForServerTLS(func() (*api.PublicAccessInfo, error) {
			return s.client.GetDatabasePublicAccess(s.envID, s.serviceID)
		}, mysqlTLSPollInterval, mysqlTLSWaitTimeout)
		return werr
	})
	if err != nil {
		if api.StatusCode(err) != 0 {
			return explainPublicAccessError(err, "wait for the TLS certificate", false)
		}
		return err
	}
	s.info = ready
	s.routeMayLag = true
	return nil
}

// normalizeAllowCIDRs valida e canonicaliza as origens vindas de --allow, com a
// mesma regra do orchestrator (IP solto vira /32 ou /128, prefixo é normalizado,
// duplicata sai). Validar aqui evita round-trip só pra receber 400.
func normalizeAllowCIDRs(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		var prefix netip.Prefix
		if addr, err := netip.ParseAddr(entry); err == nil {
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		} else {
			parsed, perr := netip.ParsePrefix(entry)
			if perr != nil {
				return nil, fmt.Errorf("invalid --allow %q: use an IP (203.0.113.7) or CIDR (203.0.113.0/24)", entry)
			}
			prefix = parsed.Masked()
		}
		canonical := prefix.String()
		if _, dup := seen[canonical]; dup {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out, nil
}

// resolvePublicAccessAllowList decide a lista a enviar num `db public enable`:
// --any zera (aberto), --allow substitui, nenhum dos dois PRESERVA a lista atual
// — abrir um banco restrito nunca acontece por omissão.
func resolvePublicAccessAllowList(current *api.PublicAccessInfo) ([]string, error) {
	if dbAllowAny {
		if len(dbAllowCIDRs) > 0 {
			return nil, fmt.Errorf("--any and --allow are mutually exclusive")
		}
		return nil, nil
	}
	if len(dbAllowCIDRs) > 0 {
		return normalizeAllowCIDRs(dbAllowCIDRs)
	}
	if current != nil && current.Enabled {
		return current.AllowedCidrs, nil
	}
	return nil, nil
}

// printDatabasePublicAccess mostra o estado do endpoint em formato humano ou JSON.
// Postgres mantém o bloco histórico (host, porta, origens); MySQL acrescenta
// usuário, banco e o estado do certificado, que um cliente gráfico pede campo a
// campo. A senha só sai no JSON e em `db connect --print`.
func printDatabasePublicAccess(info *api.PublicAccessInfo, format ui.OutputFormat) {
	if format == ui.FormatJSON {
		ui.PrintJSON(info)
		return
	}
	if !info.Enabled {
		ui.PrintKeyValue("Public access", "disabled")
		if info.IsMySQL() && !info.ServerTLSReady {
			ui.PrintInfo("enabling restarts this MySQL once (about 1 minute) to load its TLS certificate")
		}
		return
	}
	access := "any IP"
	if len(info.AllowedCidrs) > 0 {
		access = strings.Join(info.AllowedCidrs, ", ")
	}
	if !info.IsMySQL() {
		ui.PrintKeyValue(
			"Public access", "enabled",
			"Host", info.Host,
			"Port", fmt.Sprintf("%d", info.Port),
			"Allowed from", access,
		)
		return
	}
	tls := "ready — clients verify the server identity"
	if !info.ServerTLSReady {
		tls = "preparing — the database is restarting once to load its certificate"
	}
	ui.PrintKeyValue(
		"Public access", "enabled",
		"Engine", "MySQL",
		"Host", info.Host,
		"Port", fmt.Sprintf("%d", info.Port),
		"User", info.Username,
		"Database", info.Database,
		"Allowed from", access,
		"TLS", tls,
	)
	fmt.Println()
	ui.PrintInfo("connect with: " + dbCommandHint("connect") + "   (details for GUI clients: " + dbCommandHint("connect --print") + ")")
}

// dbCommandHint monta a sugestão de comando preservando o -s do usuário.
func dbCommandHint(sub string) string {
	hint := "upuai db " + sub
	if dbServiceRef != "" {
		hint += " -s " + dbServiceRef
	}
	return hint
}

// fetchPublicAccessFor lê o estado do endpoint de um banco já resolvido.
func fetchPublicAccessFor(client *api.Client, envID, serviceID string) (*api.PublicAccessInfo, error) {
	var info *api.PublicAccessInfo
	if err := ui.RunWithSpinner("Fetching access status...", func() error {
		var apiErr error
		info, apiErr = client.GetDatabasePublicAccess(envID, serviceID)
		return apiErr
	}); err != nil {
		return nil, explainPublicAccessError(err, "get public access", false)
	}
	return info, nil
}

func runDBPublicStatus(cmd *cobra.Command, args []string) error {
	if err := requireAuth(); err != nil {
		return err
	}
	client := api.NewClient()
	envID, serviceID, err := resolveDatabaseService(client, dbServiceRef)
	if err != nil {
		return err
	}
	info, err := fetchPublicAccessFor(client, envID, serviceID)
	if err != nil {
		return err
	}
	printDatabasePublicAccess(info, getOutputFormat())
	return nil
}

var dbPublicCmd = &cobra.Command{
	Use:   "public",
	Short: "Inspect the public endpoint of the linked database",
	Long: `Print whether the database is reachable from the internet and which origins
may connect (same as 'upuai db public status').

PostgreSQL is published on <host>:5432. Each MySQL gets its own port on
<host> (stable across disable/enable); the output adds the user, the database
and whether the TLS certificate is loaded.

Use 'enable' / 'disable' to change it.`,
	RunE: runDBPublicStatus,
}

var dbPublicStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the public endpoint of the linked database and who may connect",
	Args:  cobra.NoArgs,
	RunE:  runDBPublicStatus,
}

// publicEnableWarnings lista o que `db public enable` precisa confirmar.
// restart: MySQL anterior ao endpoint público reinicia uma vez ao ligar.
func publicEnableWarnings(current *api.PublicAccessInfo, allow []string) (warnings []string, restart bool) {
	if current.IsMySQL() && !current.Enabled && !current.ServerTLSReady {
		restart = true
		warnings = append(warnings, "Enabling public access restarts this MySQL once (about 1 minute of downtime) to load its TLS certificate.")
	}
	if len(allow) == 0 {
		warnings = append(warnings, "This exposes the database to ANY IP on the internet (password + TLS only).")
	}
	return warnings, restart
}

var dbPublicEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Publish the database endpoint (optionally restricted by IP)",
	Long: `Publish the database on the internet and choose who may connect.

  upuai db public enable --allow 203.0.113.7 --allow 10.0.0.0/8
      Only those origins connect; anything else is refused at the edge.

  upuai db public enable --any
      Open to any IP — password and TLS are the only barrier.

  upuai db public enable
      Publishes keeping the current allowlist. An endpoint that is already
      restricted is never opened by omission.

MySQL: the database gets its own public port (kept across disable/enable) and
TLS is verified against the server identity. A MySQL created before public
access restarts ONCE (about 1 minute) the first time it is enabled, to load the
certificate: that needs --yes or an interactive confirmation, and the command
waits until the database is ready again (up to 3 minutes). Right after enabling,
the edge takes a few seconds to route the new port.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		client := api.NewClient()
		envID, serviceID, err := resolveDatabaseService(client, dbServiceRef)
		if err != nil {
			return err
		}
		current, err := fetchPublicAccessFor(client, envID, serviceID)
		if err != nil {
			return err
		}
		allow, err := resolvePublicAccessAllowList(current)
		if err != nil {
			return err
		}
		if warnings, restart := publicEnableWarnings(current, allow); len(warnings) > 0 && !flagYes {
			for _, w := range warnings {
				ui.PrintWarning(w)
			}
			// O restart é disruptivo: sem TTY, pede --yes em vez de morrer no
			// /dev/tty do prompt.
			if restart && !stdinIsTerminal() {
				return fmt.Errorf("enabling public access restarts this MySQL once (about 1 minute) — re-run with --yes to confirm (non-interactive)")
			}
			confirmed, cerr := ui.Confirm("Continue?")
			if cerr != nil {
				return cerr
			}
			if !confirmed {
				ui.PrintInfo("aborted")
				return nil
			}
		}
		var info *api.PublicAccessInfo
		if err := ui.RunWithSpinner("Applying public access...", func() error {
			var apiErr error
			info, apiErr = client.SetDatabasePublicAccess(envID, serviceID, true, allow)
			return apiErr
		}); err != nil {
			return explainPublicAccessError(err, "enable public access", true)
		}
		ui.PrintSuccess("public access enabled")
		if info.IsMySQL() && !info.ServerTLSReady {
			sess := &publicAccessSession{client: client, envID: envID, serviceID: serviceID, info: info}
			if err := sess.waitForServerTLS(); err != nil {
				return err
			}
			info = sess.info
		}
		printDatabasePublicAccess(info, getOutputFormat())
		return nil
	},
}

var dbPublicDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Remove the public endpoint of the linked database",
	Long: `Unpublish the database. The route and the IP allowlist are removed; the
database keeps running and stays reachable from inside the platform. A MySQL
keeps its public port reserved: enabling again publishes the same host and port.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		client := api.NewClient()
		envID, serviceID, err := resolveDatabaseService(client, dbServiceRef)
		if err != nil {
			return err
		}
		var info *api.PublicAccessInfo
		if err := ui.RunWithSpinner("Disabling public access...", func() error {
			var apiErr error
			info, apiErr = client.SetDatabasePublicAccess(envID, serviceID, false, nil)
			return apiErr
		}); err != nil {
			return explainPublicAccessError(err, "disable public access", true)
		}
		ui.PrintSuccess("public access disabled")
		printDatabasePublicAccess(info, getOutputFormat())
		return nil
	},
}

// explainPublicAccessError: além dos códigos do catálogo (explainDatabaseError),
// o 403 que importa aqui. O GET devolve a credencial, então a API o trata como
// entrega de escrita: um token de API de escopo READ recebe 403 sem `code`
// (api-token-scope-guard). Sem esta tradução o usuário via "Insufficient
// permissions"/"read-only" e não sabia que o token é que precisa de deploy.
func explainPublicAccessError(err error, action string, write bool) error {
	if api.StatusCode(err) == http.StatusForbidden && api.ErrorCode(err) == "" {
		var msg string
		switch {
		case config.MachineTokenFromEnv() != "" && !write:
			msg = fmt.Sprintf("this token can read but public access returns credentials — use a token with deploy scope (%s: upuai token create --scope deploy)", config.EnvTokenVar)
		case config.MachineTokenFromEnv() != "":
			// Escopo DEPLOY vale como ADMIN do workspace: o 403 aqui é o READ.
			msg = fmt.Sprintf("this token is read-only — changing public access needs a token with deploy scope (%s: upuai token create --scope deploy)", config.EnvTokenVar)
		case write:
			msg = "changing public access requires the owner or admin role in this workspace"
		}
		if msg != "" {
			var apiErr *api.APIError
			if errors.As(err, &apiErr) && apiErr.RequestID != "" {
				msg += fmt.Sprintf(" (requestId: %s)", apiErr.RequestID)
			}
			return &databaseCommandError{msg: msg, cause: err}
		}
	}
	return explainDatabaseError(err, "", action)
}

// runLibpqTool exec um binário libpq (psql/pg_dump/pg_restore) capturando stderr
// pra que possamos detectar erros recorrentes (version mismatch, faltando binário,
// SSL trust) e emitir mensagens acionáveis em vez do "exit 1" cru. stderr é
// também espelhado no console em tempo real pra não engolir output útil.
func runLibpqTool(toolName string, c *exec.Cmd) error {
	var stderr strings.Builder
	c.Stderr = io.MultiWriter(os.Stderr, &stderr)
	err := c.Run()
	if err == nil {
		return nil
	}
	stderrText := stderr.String()
	switch {
	case strings.Contains(stderrText, "server version mismatch"):
		// pg_dump/pg_restore exigem client >= server. Mensagem padrão é confusa
		// ("aborting because of server version mismatch") sem dizer o que fazer.
		return fmt.Errorf(
			"%s aborted: client version is older than the server. "+
				"Update postgresql-client to match (macOS: brew install postgresql@latest)",
			toolName,
		)
	case errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found"):
		return fmt.Errorf("%s not found — install postgresql-client (macOS: brew install postgresql)", toolName)
	default:
		return fmt.Errorf("%s failed: %w", toolName, err)
	}
}

// withSystemTrustStore garante que a connection string use o trust store do OS
// quando sslmode=verify-full. libpq por padrão exige `~/.postgresql/root.crt`,
// que ninguém tem em macOS/Linux/Windows fresh — sem `sslrootcert=system` o
// psql/pg_dump/pg_restore falha out-of-the-box. O orchestrator novo já inclui,
// mas mantemos isso como safety-net pra clientes apontando pra orchestrators
// antigos. Idempotente: no-op se já tiver `sslrootcert=`.
func withSystemTrustStore(connStr string) string {
	if !strings.Contains(connStr, "sslmode=verify-full") {
		return connStr
	}
	if strings.Contains(connStr, "sslrootcert=") {
		return connStr
	}
	return connStr + "&sslrootcert=system"
}

// resolveDatabaseService returns (envID, dbServiceID) for the db subcommands.
//
// Precedência (em ordem):
//  1. ref != "" → resolve via project services, valida que type == database.
//  2. linked service em .upuai/config.json E type == database → usa direto.
//  3. fallback: lista services do project no env atual, filtra por type == database:
//     0 → erro com hint pra `--service`; 1 → usa; N → erro listando candidatos.
//
// Por que NÃO usar requireServiceConfig() literal: o linked service costuma ser
// uma app/web (caso comum). Com isso, o ID ia direto pro endpoint de DB e o
// orchestrator falhava com 500 ("CNPG cluster not found"). Resolver pelo project
// torna `upuai db connect` independente do que o repo está linkado.
func resolveDatabaseService(client *api.Client, ref string) (envID, serviceID string, err error) {
	// (1) Override explícito via -s/--service.
	if ref != "" {
		envID, serviceID, err = resolveServiceContext(ref)
		if err != nil {
			return "", "", err
		}
		svc, lookupErr := findServiceByID(client, serviceID)
		if lookupErr != nil {
			return "", "", lookupErr
		}
		if svc.Type != databaseServiceType {
			return "", "", fmt.Errorf("service %q has type %q, not %q — db commands only work on managed databases", svc.Name, svc.Type, databaseServiceType)
		}
		return envID, serviceID, nil
	}

	// (2) Linked service já é um banco — atalho rápido.
	if cfg, _ := config.LoadProjectConfig(); cfg != nil && cfg.EnvironmentID != "" && cfg.ServiceID != "" {
		svc, lookupErr := findServiceByID(client, cfg.ServiceID)
		if lookupErr == nil && svc.Type == databaseServiceType {
			return cfg.EnvironmentID, cfg.ServiceID, nil
		}
	}

	// (3) Auto-resolve pelo project: precisa de project + env.
	projectID, err := requireProject()
	if err != nil {
		return "", "", err
	}
	envID, err = resolveEnvironmentID(client, projectID)
	if err != nil {
		return "", "", err
	}

	services, err := client.ListServices(projectID)
	if err != nil {
		return "", "", fmt.Errorf("list services: %w", err)
	}
	dbs := make([]api.AppService, 0, 2)
	for _, s := range services {
		if s.Type == databaseServiceType {
			dbs = append(dbs, s)
		}
	}
	switch len(dbs) {
	case 0:
		return "", "", fmt.Errorf("no database service found in this project — add one with 'upuai add --type database'")
	case 1:
		return envID, dbs[0].ID, nil
	default:
		names := make([]string, 0, len(dbs))
		for _, d := range dbs {
			names = append(names, d.Name)
		}
		return "", "", fmt.Errorf("multiple databases in project (%s) — pick one with --service <name>", strings.Join(names, ", "))
	}
}

// findServiceByID localiza um service pelo ID na listagem do project linkado.
// Usado como source-of-truth do Service.type sem precisar de endpoint singleton.
func findServiceByID(client *api.Client, serviceID string) (*api.AppService, error) {
	projectID, err := requireProject()
	if err != nil {
		return nil, err
	}
	services, err := client.ListServices(projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	for _, s := range services {
		if s.ID == serviceID {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("service %s not found in project", serviceID)
}

func init() {
	// Note: --out has no short flag because -o is reserved for the global --output.
	dbBackupCmd.Flags().StringVar(&dbBackupOut, "out", "", "Output path (required for PostgreSQL .dump; MySQL defaults to <service>-<UTC timestamp>.sql)")
	dbRestoreCmd.Flags().StringVarP(&dbRestoreIn, "file", "f", "", "Input dump file (.dump for PostgreSQL, .sql for MySQL); or pass it as the argument")
	dbConnectCmd.Flags().BoolVar(&dbConnectPrint, "print", false, "Print the connection string instead of opening psql/mysql")
	for _, c := range []*cobra.Command{dbConnectCmd, dbBackupCmd, dbRestoreCmd} {
		c.Flags().BoolVar(&dbAutoEnable, "enable", false, "Auto-enable public access without prompting if currently disabled")
		c.Flags().StringVarP(&dbServiceRef, "service", "s", "", "Database service name, slug, or ID (overrides project auto-resolve)")
	}

	dbPublicEnableCmd.Flags().StringArrayVar(&dbAllowCIDRs, "allow", nil, "Origin allowed to connect (IP or CIDR). Repeatable; replaces the current list")
	dbPublicEnableCmd.Flags().BoolVar(&dbAllowAny, "any", false, "Allow any IP (clears the allowlist)")
	for _, c := range []*cobra.Command{dbPublicCmd, dbPublicStatusCmd, dbPublicEnableCmd, dbPublicDisableCmd} {
		c.Flags().StringVarP(&dbServiceRef, "service", "s", "", "Database service name, slug, or ID (overrides project auto-resolve)")
	}
	dbPublicCmd.AddCommand(dbPublicStatusCmd)
	dbPublicCmd.AddCommand(dbPublicEnableCmd)
	dbPublicCmd.AddCommand(dbPublicDisableCmd)
	dbCmd.AddCommand(dbPublicCmd)
	dbCmd.AddCommand(dbConnectCmd)
	dbCmd.AddCommand(dbBackupCmd)
	dbCmd.AddCommand(dbRestoreCmd)
	rootCmd.AddCommand(dbCmd)
}
