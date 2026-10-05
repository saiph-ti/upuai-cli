package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

// `upuai db credentials ...` cuida da conta de aplicação de um MySQL gerenciado.
// A plataforma cria a conta e guarda a credencial em uso; as variáveis do
// serviço do banco (MYSQL_USER, MYSQL_PASSWORD, DATABASE_URL, ...) a espelham e
// são somente leitura. Trocar a senha é aqui — nunca editando a variável.

var dbCredentialsCmd = &cobra.Command{
	Use:     "credentials",
	Aliases: []string{"creds"},
	Short:   "Repair or rotate the application account of a managed MySQL database",
	Long: `Manage the account your services use to connect to a managed MySQL database.

The platform creates the account and keeps the credential in use. The database
service variables (MYSQL_USER, MYSQL_PASSWORD, DATABASE_URL, ...) mirror it and
are read-only: read them with 'upuai variables list -s <database>', and reference them
from your services with ${{<database>.DATABASE_URL}} so a password change reaches
them on the next deploy.

Examples:
  upuai db credentials repair           Check the login; re-apply the account if the database refuses it
  upuai db credentials rotate           Generate a new password (asks for confirmation)
  upuai db credentials rotate --yes     Same, non-interactive`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var dbCredentialsRepairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Check that the account can log in; re-apply it if the database refuses it",
	Long: `Prove that the advertised account can log in to the database and, if the server
refuses it (the account is missing or its password was changed outside the
platform), apply it again. The password is not changed.

Services listed in the result carry the credential and need a redeploy to
receive the current value.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCredentialsMutation("Checking database credentials...", "repair credentials",
			func(c *api.Client, envID, serviceID string) (*api.DatabaseCredentialsResult, error) {
				return c.RepairDatabaseCredentials(envID, serviceID)
			})
	},
}

var dbCredentialsRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Generate a new password for the application account",
	Long: `Generate a new password for the account of a managed MySQL database.

The current password stops opening connections immediately; connections already
open keep working. The new value is written to the database service variables.
Services listed in the result keep the old password until they are redeployed.
Asks for confirmation unless --yes is passed. Requires workspace owner or admin.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !flagYes {
			// Sem TTY o ui.Confirm tentaria abrir /dev/tty e falharia com erro
			// críptico — mesma regra dos outros comandos destrutivos.
			if !stdinIsTerminal() {
				return fmt.Errorf("rotating the password needs confirmation — re-run with --yes (non-interactive)")
			}
			confirmed, err := ui.Confirm("Rotate the database password? The current one stops opening connections immediately; services keep it until redeployed.")
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintWarning("aborted")
				return nil
			}
		}
		return runCredentialsMutation("Rotating database password...", "rotate credentials",
			func(c *api.Client, envID, serviceID string) (*api.DatabaseCredentialsResult, error) {
				return c.RotateDatabaseCredentials(envID, serviceID)
			})
	},
}

func runCredentialsMutation(
	spinner, action string,
	call func(c *api.Client, envID, serviceID string) (*api.DatabaseCredentialsResult, error),
) error {
	client, envID, serviceID, err := resolveDatabaseTarget()
	if err != nil {
		return err
	}
	var res *api.DatabaseCredentialsResult
	if err := ui.RunWithSpinner(spinner, func() error {
		var apiErr error
		res, apiErr = call(client, envID, serviceID)
		return apiErr
	}); err != nil {
		return explainDatabaseError(err, "", action)
	}
	printCredentialsResult(res, getOutputFormat())
	return nil
}

func printCredentialsResult(res *api.DatabaseCredentialsResult, format ui.OutputFormat) {
	if format == ui.FormatJSON {
		ui.PrintJSON(res)
		return
	}
	switch {
	case res.Rotated:
		ui.PrintSuccess("password rotated — the previous one no longer opens connections")
	case res.Repaired:
		ui.PrintSuccess("account re-applied on the database")
	case len(res.AffectedServices) > 0:
		ui.PrintSuccess("credentials are working; the stored values were brought up to date")
	default:
		ui.PrintSuccess("credentials are working — nothing to change")
		return
	}
	// AffectedServices só vem quando o VALOR da credencial mudou.
	if len(res.AffectedServices) == 0 {
		if res.Rotated {
			ui.PrintInfo("no service in this environment uses the credentials of this database")
		} else {
			ui.PrintInfo("the password did not change — no redeploy needed")
		}
		return
	}
	names := make([]string, 0, len(res.AffectedServices))
	for _, s := range res.AffectedServices {
		names = append(names, s.Name)
	}
	ui.PrintInfo(fmt.Sprintf("redeploy to pick up the current credential: %s", strings.Join(names, ", ")))
	ui.PrintInfo("read the new value with 'upuai variables list -s <database>'")
}

func init() {
	for _, c := range []*cobra.Command{dbCredentialsRepairCmd, dbCredentialsRotateCmd} {
		c.Flags().StringVarP(&dbServiceRef, "service", "s", "", "Database service name, slug, or ID (overrides project auto-resolve)")
	}
	dbCredentialsCmd.AddCommand(dbCredentialsRepairCmd)
	dbCredentialsCmd.AddCommand(dbCredentialsRotateCmd)
	dbCmd.AddCommand(dbCredentialsCmd)
}
