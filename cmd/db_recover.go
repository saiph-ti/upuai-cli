package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	dbRecoverTo   string
	dbRecoverName string
)

var dbRecoveryWindowCmd = &cobra.Command{
	Use:   "recovery-window",
	Short: "Show the point-in-time restore window of the linked PostgreSQL database",
	Long: `Show from when to when the linked PostgreSQL database can be restored to a
point in time: from the oldest retained daily backup to the last archived WAL.
The window is the plan's backup retention (paid plans). CLI v0.31.0+.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		client := api.NewClient()
		envID, serviceID, err := resolveDatabaseService(client, dbServiceRef)
		if err != nil {
			return err
		}
		w, err := client.GetDatabaseRecoveryWindow(envID, serviceID)
		if err != nil {
			return explainDatabaseError(err, "", "read the recovery window")
		}
		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(w)
			return nil
		}
		fmt.Println()
		if !w.Available {
			ui.PrintKeyValue("Point-in-time restore", "not available yet", "Reason", w.UnavailableReason)
			fmt.Println()
			ui.PrintInfo("Available after the first daily backup, on paid plans.")
			fmt.Println()
			return nil
		}
		ui.PrintKeyValue(
			"Point-in-time restore", "available",
			"From", localTime(w.FirstRecoverabilityPoint),
			"To", localTime(w.LatestRestorableTime),
		)
		fmt.Println()
		ui.PrintInfo("Restore with: " + ui.Accent.Render("upuai db recover --to <time>"))
		fmt.Println()
		return nil
	},
}

var dbRecoverCmd = &cobra.Command{
	Use:   "recover --to <time>",
	Short: "Restore the linked PostgreSQL database to a point in time, into a NEW database",
	Long: `Create a NEW PostgreSQL database in the same environment with the data of the
linked database at the given moment. The current database is not changed; the
new one gets its own credentials and shows up on the canvas.

  upuai db recover --to 2026-11-23T09:30:00-03:00
  upuai db recover --to 2026-11-23T12:30:00Z --name orders-before-bug

--to is RFC 3339 and must be inside 'upuai db recovery-window'. The restore
runs in the background: follow it with 'upuai status'. Owner or admin role
(API tokens: deploy scope). CLI v0.31.0+.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		target, err := time.Parse(time.RFC3339, dbRecoverTo)
		if err != nil {
			return fmt.Errorf("--to must be RFC 3339, e.g. 2026-11-23T09:30:00-03:00")
		}
		client := api.NewClient()
		envID, serviceID, err := resolveDatabaseService(client, dbServiceRef)
		if err != nil {
			return err
		}
		var r *api.RestoreDatabaseResult
		if err := ui.RunWithSpinner("Starting restore...", func() error {
			var apiErr error
			r, apiErr = client.RestoreDatabase(envID, serviceID, target.Format(time.RFC3339), dbRecoverName)
			return apiErr
		}); err != nil {
			return explainDatabaseError(err, "", "restore the database")
		}
		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(r)
			return nil
		}
		ui.PrintSuccess(fmt.Sprintf("restore started: new database %q (data as of %s)", r.Name, localTime(r.TargetTime)))
		ui.PrintInfo("It becomes available when the restore finishes — follow it with 'upuai status'.")
		return nil
	},
}

// localTime mostra um instante RFC 3339 no fuso local (mesmo valor, legível).
func localTime(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return t.Local().Format("2006-01-02 15:04:05 MST")
}

func init() {
	for _, c := range []*cobra.Command{dbRecoveryWindowCmd, dbRecoverCmd} {
		c.Flags().StringVarP(&dbServiceRef, "service", "s", "", "Database service name, slug, or ID (overrides project auto-resolve)")
	}
	dbRecoverCmd.Flags().StringVar(&dbRecoverTo, "to", "", "Point in time to restore to (RFC 3339)")
	dbRecoverCmd.Flags().StringVar(&dbRecoverName, "name", "", "Name of the new database (default: <source>-restore-<date>)")
	_ = dbRecoverCmd.MarkFlagRequired("to")
	dbCmd.AddCommand(dbRecoveryWindowCmd, dbRecoverCmd)
}
