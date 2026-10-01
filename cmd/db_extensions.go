package cmd

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

// `upuai db extensions ...` gerencia a allowlist curada de extensões Postgres
// da plataforma (PostGIS, pgvector, pg_trgm, ...) no banco `app` — o mesmo da
// connection string. Ativar é instantâneo e não reinicia o banco; extensões que
// a imagem em execução não traz pedem antes a atualização de manutenção
// (`upuai db update`).

// extensionDescriptionWidth limita a coluna DESCRIPTION da tabela: o comment do
// Postgres às vezes passa de 100 caracteres e quebraria a linha no terminal.
const extensionDescriptionWidth = 60

var dbExtensionsCmd = &cobra.Command{
	Use:     "extensions",
	Aliases: []string{"ext", "extension"},
	Short:   "List the managed Postgres extensions of the linked database",
	Long: `List the Postgres extensions the platform manages (PostGIS, pgvector, pg_trgm, ...)
and their state in the 'app' database.

STATUS is one of:
  enabled       installed in the database
  available     shipped by the database image, not installed
  unavailable   not shipped by the current image — run 'upuai db update' first

Examples:
  upuai db extensions                    List extensions
  upuai db extensions enable postgis     Enable PostGIS (instant, no restart)
  upuai db extensions disable postgis    Disable it (refused while anything depends on it)
  upuai db extensions update postgis     Update to the version shipped by the image`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, envID, serviceID, err := resolveDatabaseTarget()
		if err != nil {
			return err
		}
		var list *api.DatabaseExtensionList
		if err := ui.RunWithSpinner("Fetching extensions...", func() error {
			var apiErr error
			list, apiErr = client.ListDatabaseExtensions(envID, serviceID)
			return apiErr
		}); err != nil {
			return explainDatabaseError(err, "", "list extensions")
		}
		printDatabaseExtensions(list, getOutputFormat())
		return nil
	},
}

var dbExtensionsEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a Postgres extension (CREATE EXTENSION, no restart)",
	Long: `Enable a managed Postgres extension in the 'app' database.

Runs CREATE EXTENSION ... CASCADE: required extensions are installed too (e.g.
postgis_topology brings postgis). It is instant and does not restart the database.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := normalizeExtensionName(args[0])
		return runExtensionMutation(name, "Enabling", "enable", func(c *api.Client, envID, serviceID string) (*api.DatabaseExtensionList, error) {
			return c.EnableDatabaseExtension(envID, serviceID, name)
		}, func(ext *api.DatabaseExtension) string {
			return fmt.Sprintf("%s %s enabled", ext.Name, ext.InstalledVersion)
		})
	},
}

var dbExtensionsDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a Postgres extension (DROP EXTENSION ... RESTRICT)",
	Long: `Disable a managed Postgres extension in the 'app' database.

Runs DROP EXTENSION ... RESTRICT — never CASCADE. The objects owned by the
extension itself are removed (for PostGIS that includes spatial_ref_sys), and the
command is refused while any column, view or function of yours still depends on
it. Asks for confirmation unless --yes is passed.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := normalizeExtensionName(args[0])
		if !flagYes {
			// Sem TTY o ui.Confirm tentaria abrir /dev/tty e falharia com erro
			// críptico — mesma regra do `db connect` em scripts.
			if !stdinIsTerminal() {
				return fmt.Errorf("disabling %s needs confirmation — re-run with --yes (non-interactive)", name)
			}
			confirmed, err := ui.Confirm(fmt.Sprintf("Disable extension %s? Objects owned by the extension are dropped; it is refused while anything of yours depends on it.", name))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintWarning("aborted")
				return nil
			}
		}
		return runExtensionMutation(name, "Disabling", "disable", func(c *api.Client, envID, serviceID string) (*api.DatabaseExtensionList, error) {
			return c.DisableDatabaseExtension(envID, serviceID, name)
		}, func(ext *api.DatabaseExtension) string {
			return fmt.Sprintf("%s disabled", name)
		})
	},
}

var dbExtensionsUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update an installed extension to the version shipped by the image",
	Long: `Update an installed Postgres extension to the version shipped by the current
database image (ALTER EXTENSION ... UPDATE; for the PostGIS family,
postgis_extensions_upgrade()). 'upuai db extensions' shows which ones have an
update available.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := normalizeExtensionName(args[0])
		return runExtensionMutation(name, "Updating", "update", func(c *api.Client, envID, serviceID string) (*api.DatabaseExtensionList, error) {
			return c.UpdateDatabaseExtension(envID, serviceID, name)
		}, func(ext *api.DatabaseExtension) string {
			return fmt.Sprintf("%s is at %s", ext.Name, ext.InstalledVersion)
		})
	},
}

// normalizeExtensionName: nomes da allowlist são minúsculos (postgis,
// uuid-ossp); aceitar "PostGIS" evita um 400 por capitalização.
func normalizeExtensionName(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// resolveDatabaseTarget autentica e resolve o banco-alvo (-s ou auto-resolve).
func resolveDatabaseTarget() (*api.Client, string, string, error) {
	if err := requireAuth(); err != nil {
		return nil, "", "", err
	}
	client := api.NewClient()
	envID, serviceID, err := resolveDatabaseService(client, dbServiceRef)
	if err != nil {
		return nil, "", "", err
	}
	return client, envID, serviceID, nil
}

// runExtensionMutation é o esqueleto comum de enable/disable/update: resolve o
// banco, chama a API com spinner, traduz o erro e imprime a lista resultante
// (JSON) ou uma linha de sucesso (texto).
func runExtensionMutation(
	name, verb, action string,
	call func(c *api.Client, envID, serviceID string) (*api.DatabaseExtensionList, error),
	success func(ext *api.DatabaseExtension) string,
) error {
	if name == "" {
		return fmt.Errorf("extension name is required — run 'upuai db extensions' to see the list")
	}
	client, envID, serviceID, err := resolveDatabaseTarget()
	if err != nil {
		return err
	}
	var list *api.DatabaseExtensionList
	if err := ui.RunWithSpinner(fmt.Sprintf("%s %s...", verb, name), func() error {
		var apiErr error
		list, apiErr = call(client, envID, serviceID)
		return apiErr
	}); err != nil {
		return explainDatabaseError(err, name, action+" "+name)
	}
	if getOutputFormat() == ui.FormatJSON {
		ui.PrintJSON(list)
		return nil
	}
	ext := findExtension(list, name)
	if ext == nil {
		// disable devolve a lista sem a extensão instalada, mas ela continua no
		// catálogo; ausência total só ocorre se o contrato mudar — não inventa
		// versão, reporta o essencial.
		ext = &api.DatabaseExtension{Name: name}
	}
	ui.PrintSuccess(success(ext))
	return nil
}

func findExtension(list *api.DatabaseExtensionList, name string) *api.DatabaseExtension {
	if list == nil {
		return nil
	}
	for i := range list.Extensions {
		if list.Extensions[i].Name == name {
			return &list.Extensions[i]
		}
	}
	return nil
}

// extensionStatus resume o estado para a coluna STATUS.
func extensionStatus(ext api.DatabaseExtension) string {
	switch {
	case ext.Installed:
		return "enabled"
	case ext.Available:
		return "available"
	default:
		return "unavailable"
	}
}

// extensionVersion mostra a versão instalada (com a seta para a da imagem
// quando há atualização) ou, se não instalada, a versão que seria instalada.
func extensionVersion(ext api.DatabaseExtension) string {
	switch {
	case ext.Installed && ext.UpdateAvailable:
		return fmt.Sprintf("%s → %s", ext.InstalledVersion, ext.DefaultVersion)
	case ext.Installed:
		return ext.InstalledVersion
	case ext.Available:
		return ext.DefaultVersion
	default:
		return "—"
	}
}

func truncateText(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}

func printDatabaseExtensions(list *api.DatabaseExtensionList, format ui.OutputFormat) {
	if format == ui.FormatJSON {
		ui.PrintJSON(list)
		return
	}
	if list == nil || len(list.Extensions) == 0 {
		ui.PrintInfo("No managed extensions reported for this database.")
		return
	}
	table := ui.NewTable("Name", "Category", "Status", "Version", "Description")
	unavailable, updatable := 0, 0
	for _, ext := range list.Extensions {
		if !ext.Available && !ext.Installed {
			unavailable++
		}
		if ext.UpdateAvailable {
			updatable++
		}
		table.AddRow(ext.Name, ext.Category, extensionStatus(ext), extensionVersion(ext), truncateText(ext.Description, extensionDescriptionWidth))
	}
	table.Print()
	if unavailable > 0 {
		fmt.Println()
		ui.PrintInfo(fmt.Sprintf("%d extension(s) need a database update first — run 'upuai db update'", unavailable))
	}
	if updatable > 0 {
		fmt.Println()
		ui.PrintInfo("Update an installed extension with 'upuai db extensions update <name>'")
	}
}

// databaseErrorReasons: os `reason` de ConflictError da API (code
// OPERATION_NOT_ALLOWED) que estes comandos podem receber.
const (
	reasonDeploymentInProgress = "deploymentInProgress"
	reasonManagedDatabaseOnly  = "managedDatabaseOnly"
	reasonNoDatabaseEngine     = "noDatabaseEngine"
	reasonPostgresOnly         = "postgresOnly"
)

// explainDatabaseError traduz os códigos estáveis da API em orientação
// acionável. Ramifica por `code`/`details.reason`, nunca por `message` (texto
// de UI, reescrito sem aviso). Códigos desconhecidos mantêm o erro original com
// o contexto da ação — sem engolir requestId.
func explainDatabaseError(err error, extName, action string) error {
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("%s: %w", action, err)
	}
	detail := func(key string) string {
		if apiErr.Details == nil {
			return ""
		}
		return apiErr.Details[key]
	}
	subject := extName
	if subject == "" {
		subject = detail("extension")
	}
	var msg string
	switch apiErr.Code {
	case "DB_EXTENSION_UNAVAILABLE":
		if detail("updateAvailable") == "true" {
			msg = fmt.Sprintf("%s is not shipped by this database's current image — apply the pending update first: upuai db update", subject)
		} else {
			msg = fmt.Sprintf("%s is not available on this database", subject)
		}
	case "DB_EXTENSION_IN_USE":
		msg = fmt.Sprintf("cannot disable %s: other objects still depend on it", subject)
		if deps := strings.TrimSpace(detail("dependents")); deps != "" {
			msg += ":\n" + indentLines(deps, "  ")
		}
		msg += "\ndrop or change those objects first — the platform never drops dependent objects (no CASCADE)"
	case "DB_EXTENSION_NOT_ALLOWED":
		msg = fmt.Sprintf("%s is not a platform-managed extension — run 'upuai db extensions' for the supported list (you can still create other extensions over SQL)", subject)
	case "DB_OPERATION_IN_PROGRESS":
		msg = "another operation (update or version change) is running on this database — wait for it to finish and retry"
	case "DB_OBJECT_LOCKED":
		msg = "the database is busy: another session holds a lock on the objects involved — retry in a moment"
	case "DB_READ_ONLY":
		msg = "the database is read-only (plan storage limit reached) — free space or upgrade the plan, then retry"
	case "DB_MAINTENANCE_REQUIRED":
		msg = "this database must apply the pending maintenance update first: upuai db update"
	case "DB_MAINTENANCE_COLLATION_RISK":
		msg = "this update changes the database's operating system image and it uses collations whose ordering would change"
		if dbs := detail("databases"); dbs != "" {
			msg += " (databases: " + dbs + ")"
		}
		msg += " — contact support to schedule it safely"
	case "DB_IMAGE_UNAVAILABLE":
		msg = "the target database image is not available on the platform right now — try again later or contact support"
	default:
		switch detail("reason") {
		case reasonDeploymentInProgress:
			msg = "a deployment is in progress for this database — wait for it to finish and retry"
		case reasonManagedDatabaseOnly, reasonNoDatabaseEngine:
			msg = "this command only works on managed database services"
		case reasonPostgresOnly:
			msg = "extensions are only available on managed PostgreSQL databases"
		default:
			return fmt.Errorf("%s: %w", action, err)
		}
	}
	if apiErr.RequestID != "" {
		msg += fmt.Sprintf(" (requestId: %s)", apiErr.RequestID)
	}
	return &databaseCommandError{msg: msg, cause: err}
}

// databaseCommandError carrega a mensagem acionável sem perder a causa: o
// api.ErrorCode/api.StatusCode continuam funcionando via errors.As (Unwrap).
type databaseCommandError struct {
	msg   string
	cause error
}

func (e *databaseCommandError) Error() string { return e.msg }
func (e *databaseCommandError) Unwrap() error { return e.cause }

func indentLines(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + strings.TrimSpace(l)
	}
	return strings.Join(lines, "\n")
}

func init() {
	for _, c := range []*cobra.Command{dbExtensionsCmd, dbExtensionsEnableCmd, dbExtensionsDisableCmd, dbExtensionsUpdateCmd} {
		c.Flags().StringVarP(&dbServiceRef, "service", "s", "", "Database service name, slug, or ID (overrides project auto-resolve)")
	}
	dbExtensionsCmd.AddCommand(dbExtensionsEnableCmd)
	dbExtensionsCmd.AddCommand(dbExtensionsDisableCmd)
	dbExtensionsCmd.AddCommand(dbExtensionsUpdateCmd)
	dbCmd.AddCommand(dbExtensionsCmd)
}
