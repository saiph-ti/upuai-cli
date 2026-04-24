package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

// `upuai db ...` wraps the Public DB Endpoint feature so customers can
// connect / dump / restore from anywhere with a one-liner.
// Runbook: upuai-core/docs/runbooks/2026-04-24-public-db-endpoint.md

var (
	dbBackupOut  string
	dbRestoreIn  string
	dbAutoEnable bool
)

var dbCmd = &cobra.Command{
	Use:     "db",
	Aliases: []string{"database"},
	Short:   "Manage the linked database (connect, backup, restore)",
	Long: `Manage the linked database service.

Examples:
  upuai db connect              Print the public connection string
  upuai db backup -o file.dump  Run pg_dump against the public endpoint
  upuai db restore -i file.dump Restore a dump via pg_restore`,
}

var dbConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Print the public connection string for the linked database",
	Long: `Fetch the public connection string for the linked database service.

If public access is currently disabled, you will be prompted to enable it
(use --yes to skip the prompt).`,
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
		ui.PrintKeyValue("Host", info.Host, "Port", fmt.Sprintf("%d", info.Port))
		fmt.Println()
		fmt.Println(info.ConnectionString)
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
		defer out.Close()

		ui.PrintInfo(fmt.Sprintf("running pg_dump → %s", dbBackupOut))
		c := exec.Command("pg_dump", "--format=custom", "--no-owner", "--no-acl", info.ConnectionString)
		c.Stdout = out
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("pg_dump failed: %w (is the postgresql client installed?)", err)
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
			"-d", info.ConnectionString,
			dbRestoreIn,
		)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("pg_restore failed: %w (is the postgresql client installed?)", err)
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
	envID, serviceID, err := requireServiceConfig()
	if err != nil {
		return nil, err
	}
	client := api.NewClient()

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

func init() {
	dbBackupCmd.Flags().StringVarP(&dbBackupOut, "out", "o", "", "Output path for the .dump file (required)")
	dbRestoreCmd.Flags().StringVarP(&dbRestoreIn, "file", "f", "", "Input .dump file path (required)")
	for _, c := range []*cobra.Command{dbConnectCmd, dbBackupCmd, dbRestoreCmd} {
		c.Flags().BoolVar(&dbAutoEnable, "enable", false, "Auto-enable public access without prompting if currently disabled")
	}

	dbCmd.AddCommand(dbConnectCmd)
	dbCmd.AddCommand(dbBackupCmd)
	dbCmd.AddCommand(dbRestoreCmd)
	rootCmd.AddCommand(dbCmd)
}
