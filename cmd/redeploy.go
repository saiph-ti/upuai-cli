package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	redeployService string
	redeployRebuild bool
)

var redeployCmd = &cobra.Command{
	Use:   "redeploy",
	Short: "Redeploy the latest deployment",
	Long: `Redeploy the latest deployment: the same commit goes live again with the
current configuration (environment variables, resources).

When nothing that goes into the build changed since a successful deploy of that
commit — build settings (builder, build and start commands, Dockerfile path and
context) and build-time variables (scope both or build) — its image is reused,
the release command runs again and no build runs. Use --rebuild to build the
commit again anyway. Multi-process (Procfile) services and upuai up deploys
always build.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		envID, serviceID, err := resolveServiceContext(redeployService)
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
			prompt := fmt.Sprintf("Redeploy %s?", latest.ID)
			if redeployRebuild {
				prompt = fmt.Sprintf("Rebuild and redeploy %s?", latest.ID)
			}
			confirmed, err := ui.Confirm(prompt)
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
			deployment, redeployErr = client.Redeploy(latest.ID, redeployRebuild)
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
	redeployCmd.Flags().StringVarP(&redeployService, "service", "s", "", "Service name, slug, or ID (overrides linked service)")
	redeployCmd.Flags().BoolVar(&redeployRebuild, "rebuild", false, "Build the commit again instead of reusing the image of an earlier deploy")
	rootCmd.AddCommand(redeployCmd)
}
