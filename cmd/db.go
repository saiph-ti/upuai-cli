package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
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
)

// databaseServiceType é o discriminador que a API usa para identificar o
// Service.type de bancos gerenciados (CNPG/MySQL/Mongo/Redis vão pelo orchestrator
// /databases). Os outros tipos (github/docker/bucket) NÃO têm CNPG cluster e
// portanto NÃO podem expor public-access — bater no endpoint com eles vira 500.
const databaseServiceType = "database"

var dbCmd = &cobra.Command{
	Use:     "db",
	Aliases: []string{"database"},
	Short:   "Manage the linked database (connect, backup, restore)",
	Long: `Manage the linked database service.

Examples:
  upuai db connect                  Open an interactive psql session
  upuai db connect --print          Print the public connection string and exit
  upuai db backup --out file.dump   Run pg_dump against the public endpoint
  upuai db restore -f file.dump     Restore a dump via pg_restore`,
}

var dbConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Open an interactive psql session against the linked database",
	Long: `Open an interactive psql shell against the linked database via the public endpoint.

Requires psql to be installed locally (libpq / postgresql-client).

Use --print to skip psql and just emit the connection string (script-friendly),
or --output json to emit the full access info object.

If public access is currently disabled, you'll be prompted to enable it (use
--yes or --enable to skip the prompt).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		info, err := loadOrEnablePublicAccess()
		if err != nil {
			return err
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(info)
			return nil
		}
		if dbConnectPrint {
			ui.PrintKeyValue("Host", info.Host, "Port", fmt.Sprintf("%d", info.Port))
			fmt.Println()
			fmt.Println(info.ConnectionString)
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
	Short: "Run pg_dump against the linked database (public endpoint)",
	Long: `Wraps pg_dump using the database's public connection string.

Requires pg_dump to be installed locally (libpq / postgresql-client).
If public access is disabled, you'll be asked to enable it first.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if dbBackupOut == "" {
			return fmt.Errorf("--out is required (path to .dump file)")
		}
		info, err := loadOrEnablePublicAccess()
		if err != nil {
			return err
		}
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
		fi, _ := os.Stat(dbBackupOut)
		ui.PrintSuccess(fmt.Sprintf("backup written: %s (%d bytes)", dbBackupOut, fi.Size()))
		return nil
	},
}

var dbRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Run pg_restore against the linked database (public endpoint)",
	Long: `Wraps pg_restore using the database's public connection string.

Requires pg_restore to be installed locally (libpq / postgresql-client).
If public access is disabled, you'll be asked to enable it first.

WARNING: pg_restore writes to the live database — make sure the file is what
you intend to restore. Default flags: --no-owner --no-acl --clean --if-exists
to drop+recreate matching objects.`,
	RunE: func(cmd *cobra.Command, args []string) error {
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
		info, err := loadOrEnablePublicAccess()
		if err != nil {
			return err
		}

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

// loadOrEnablePublicAccess fetches the current state and offers to enable it
// if disabled. Used by all three db subcommands.
func loadOrEnablePublicAccess() (*api.PublicAccessInfo, error) {
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
		return nil, fmt.Errorf("get public access: %w", err)
	}
	if info.Enabled {
		return info, nil
	}

	if !dbAutoEnable && !flagYes {
		confirmed, err := ui.Confirm("Public access is disabled. Enable it now?")
		if err != nil {
			return nil, err
		}
		if !confirmed {
			return nil, fmt.Errorf("public access required — re-run with --enable or enable in the dashboard")
		}
	}

	if err := ui.RunWithSpinner("Enabling public access...", func() error {
		var apiErr error
		info, apiErr = client.SetDatabasePublicAccess(envID, serviceID, true)
		return apiErr
	}); err != nil {
		return nil, fmt.Errorf("enable public access: %w", err)
	}
	ui.PrintSuccess(fmt.Sprintf("public access enabled at %s:%d", info.Host, info.Port))
	return info, nil
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
	dbBackupCmd.Flags().StringVar(&dbBackupOut, "out", "", "Output path for the .dump file (required)")
	dbRestoreCmd.Flags().StringVarP(&dbRestoreIn, "file", "f", "", "Input .dump file path (required)")
	dbConnectCmd.Flags().BoolVar(&dbConnectPrint, "print", false, "Print the connection string instead of opening psql")
	for _, c := range []*cobra.Command{dbConnectCmd, dbBackupCmd, dbRestoreCmd} {
		c.Flags().BoolVar(&dbAutoEnable, "enable", false, "Auto-enable public access without prompting if currently disabled")
		c.Flags().StringVarP(&dbServiceRef, "service", "s", "", "Database service name, slug, or ID (overrides project auto-resolve)")
	}

	dbCmd.AddCommand(dbConnectCmd)
	dbCmd.AddCommand(dbBackupCmd)
	dbCmd.AddCommand(dbRestoreCmd)
	rootCmd.AddCommand(dbCmd)
}
