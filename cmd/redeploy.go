package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var redeployCmd = &cobra.Command{
	Use:   "redeploy",
	Short: "Redeploy the latest deployment",
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

		// Get latest deployment
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
			ui.PrintWarning("No deployments found — run 'upuai deploy' first")
			return nil
		}

		latest := deployments[0]
		if !latest.CanRedeploy {
			return fmt.Errorf("latest deployment cannot be redeployed")
		}

		if !flagYes {
			confirmed, err := ui.Confirm(fmt.Sprintf("Redeploy %s?", latest.ID))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Redeploy cancelled")
				return nil
			}
		}

		var deployment *api.Deployment
		err = ui.RunWithSpinner("Redeploying...", func() error {
			var redeployErr error
			deployment, redeployErr = client.Redeploy(latest.ID)
			return redeployErr
		})
		if err != nil {
			return fmt.Errorf("redeploy failed: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(deployment)
			return nil
		}

		fmt.Println()
		ui.PrintSuccess("Redeployment started!")
		ui.PrintKeyValue(
			"Deployment", deployment.ID,
			"Status", deployment.Status,
		)
		if deployment.URL != "" {
			ui.PrintKeyValue("URL", deployment.URL)
		}
		fmt.Println()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(redeployCmd)
}
