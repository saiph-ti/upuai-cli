package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the linked service",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		if _, err := requireProject(); err != nil {
			return err
		}

		envID, serviceID, err := requireServiceConfig()
		if err != nil {
			return err
		}

		if !flagYes {
			confirmed, err := ui.Confirm("Restart the service?")
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Restart cancelled")
				return nil
			}
		}

		client := api.NewClient()

		err = ui.RunWithSpinner("Restarting service...", func() error {
			return client.RestartInstance(envID, serviceID)
		})
		if err != nil {
			return fmt.Errorf("restart failed: %w", err)
		}

		ui.PrintSuccess("Service restarted successfully")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(restartCmd)
}
