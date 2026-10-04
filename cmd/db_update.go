package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

// `upuai db version` / `upuai db update` — versão viva do banco e a atualização
// de manutenção: mesma versão (major), imagem atual da plataforma (patches,
// sistema base, extensões como o PostGIS). Reinicia o banco (~1–2 min numa
// instância única); se o rollout não convergir na janela, o orchestrator volta
// para a imagem anterior.

var (
	dbUpdateWait        bool
	dbUpdateWaitTimeout int
)

// dbUpdateDefaultWaitTimeout cobre a janela do rollout no orchestrator (10 min)
// com margem — maior que os 5 min do deploy de app.
const dbUpdateDefaultWaitTimeout = 15 * 60

var dbVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the running version of the linked database and pending updates",
	Long: `Show the version the linked database is actually running (read from the live
image) and whether a maintenance update is pending. Apply it with 'upuai db update'.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, envID, serviceID, err := resolveDatabaseTarget()
		if err != nil {
			return err
		}
		var v *api.DatabaseVersion
		if err := ui.RunWithSpinner("Fetching version...", func() error {
			var apiErr error
			v, apiErr = client.GetDatabaseVersion(envID, serviceID)
			return apiErr
		}); err != nil {
			return explainDatabaseError(err, "", "get database version")
		}
		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(v)
			return nil
		}
		update := "none — up to date"
		if v.UpdateAvailable {
			update = "available — run 'upuai db update'"
		}
		ui.PrintKeyValue("Engine", v.Engine, "Version", v.Version, "Update", update)
		return nil
	},
}

var dbUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Apply the pending maintenance update to the linked database (restarts it)",
	Long: `Apply the pending maintenance update: same major version, the platform's
current image (security patches, base OS and extensions such as PostGIS).

The database restarts (about 1–2 minutes of downtime on a single instance) and
is reverted to the previous image automatically if the update does not converge.
Asks for confirmation unless --yes is passed.

Examples:
  upuai db update                Apply the update and return
  upuai db update --wait         Block until it finishes (non-zero exit on failure)
  upuai db update --yes --wait   Non-interactive (CI / scripts)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDatabaseUpdate()
	},
}

func runDatabaseUpdate() error {
	client, envID, serviceID, err := resolveDatabaseTarget()
	if err != nil {
		return err
	}
	format := getOutputFormat()

	// Consulta antes de pedir confirmação: sem atualização pendente não há
	// restart a confirmar (e scripts idempotentes saem 0).
	var current *api.DatabaseVersion
	if err := ui.RunWithSpinner("Checking for updates...", func() error {
		var apiErr error
		current, apiErr = client.GetDatabaseVersion(envID, serviceID)
		return apiErr
	}); err != nil {
		return explainDatabaseError(err, "", "check database version")
	}
	if !current.UpdateAvailable {
		return printDatabaseUpToDate(&api.DatabaseMaintenanceResult{Status: "noop", Version: current.Version}, format)
	}

	if !flagYes {
		if !stdinIsTerminal() {
			return fmt.Errorf("the update restarts the database — re-run with --yes to confirm (non-interactive)")
		}
		confirmed, err := ui.Confirm(fmt.Sprintf("Apply the maintenance update to %s %s? The database restarts (about 1–2 min of downtime).", current.Engine, current.Version))
		if err != nil {
			return err
		}
		if !confirmed {
			ui.PrintWarning("aborted")
			return nil
		}
	}

	var res *api.DatabaseMaintenanceResult
	if err := ui.RunWithSpinner("Starting maintenance update...", func() error {
		var apiErr error
		res, apiErr = client.ApplyDatabaseMaintenance(envID, serviceID)
		return apiErr
	}); err != nil {
		return explainDatabaseError(err, "", "apply database update")
	}
	// Corrida benigna: outro cliente aplicou entre o GET e o POST.
	if res.Status != "accepted" || res.DeploymentID == "" {
		return printDatabaseUpToDate(res, format)
	}

	if !dbUpdateWait {
		if format == ui.FormatJSON {
			ui.PrintJSON(res)
			return nil
		}
		ui.PrintSuccess(fmt.Sprintf("Maintenance update started (deployment %s)", res.DeploymentID))
		ui.PrintInfo("Pass --wait to block until it finishes, or follow it with 'upuai status'.")
		return nil
	}

	final, err := waitForDeploymentWithin(client, res.DeploymentID, format, time.Duration(dbUpdateWaitTimeout)*time.Second)
	if err != nil {
		return err
	}
	if format == ui.FormatJSON {
		ui.PrintJSON(final)
	}
	if _, failed := failedDeployStatuses[strings.ToLower(final.Status)]; failed {
		if final.ErrorMessage != "" && format != ui.FormatJSON {
			ui.PrintError(final.ErrorMessage)
		}
		return fmt.Errorf("maintenance update %s ended with status %q — check 'upuai db version' and the deployment in the dashboard", final.ID, final.Status)
	}
	if format != ui.FormatJSON {
		ui.PrintSuccess(fmt.Sprintf("Database updated (%s %s)", current.Engine, current.Version))
	}
	return nil
}

func printDatabaseUpToDate(res *api.DatabaseMaintenanceResult, format ui.OutputFormat) error {
	if format == ui.FormatJSON {
		ui.PrintJSON(res)
		return nil
	}
	ui.PrintSuccess(fmt.Sprintf("Database already up to date (version %s)", res.Version))
	return nil
}

func init() {
	dbUpdateCmd.Flags().BoolVar(&dbUpdateWait, "wait", false, "Block until the update reaches a terminal status. Exits non-zero on failure.")
	dbUpdateCmd.Flags().IntVar(&dbUpdateWaitTimeout, "wait-timeout", dbUpdateDefaultWaitTimeout, "Maximum seconds to wait when --wait is set (0 = no limit)")
	for _, c := range []*cobra.Command{dbVersionCmd, dbUpdateCmd} {
		c.Flags().StringVarP(&dbServiceRef, "service", "s", "", "Database service name, slug, or ID (overrides project auto-resolve)")
	}
	dbCmd.AddCommand(dbVersionCmd)
	dbCmd.AddCommand(dbUpdateCmd)
}
