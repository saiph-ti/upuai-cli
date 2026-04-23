package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Remove the latest deployment (stop service)",
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

		client := api.NewClient()

		var deployments []api.Deployment
		err = ui.RunWithSpinner("Loading deployments...", func() error {
			var fetchErr error
			deployments, fetchErr = client.ListDeployments(envID, serviceID)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list deployments: %w", err)
		}

		if len(deployments) == 0 {
			ui.PrintInfo("No deployments found")
			return nil
		}

		latest := deployments[0]

		if !flagYes {
			confirmed, err := ui.Confirm(fmt.Sprintf("Remove deployment %s? This will stop the service.", latest.ID))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Cancelled")
				return nil
			}
		}

		err = ui.RunWithSpinner("Removing deployment...", func() error {
			return client.RemoveDeployment(latest.ID)
		})
		if err != nil {
			return fmt.Errorf("failed to remove deployment: %w", err)
		}

		ui.PrintSuccess("Deployment removed — service stopped")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(downCmd)
}
