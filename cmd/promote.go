package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	promoteFromFlag string
	promoteToFlag   string
)

var promoteCmd = &cobra.Command{
	Use:   "promote",
	Short: "Promote deployment between environments",
	Long: `Promote a deployment from one environment to another.

By default, promotes from staging to production.
Use --from and --to to specify custom environments.

This works by fetching the latest successful deployment from the source
environment and deploying its git info to the target environment.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		from := promoteFromFlag
		to := promoteToFlag

		if !flagYes {
			confirmed, err := ui.Confirm(
				fmt.Sprintf("Promote %s → %s?", ui.Accent.Render(from), ui.Accent.Render(to)),
			)
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Promotion cancelled")
				return nil
			}
		}

		client := api.NewClient()

		// 1. Get project status for the source environment to find envID + serviceID
		var status *api.ProjectStatus
		err = ui.RunWithSpinner("Fetching source environment...", func() error {
			var fetchErr error
			status, fetchErr = client.GetProjectStatus(projectID, from)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to get project status: %w", err)
		}

		if len(status.Environments) == 0 {
			return fmt.Errorf("environment '%s' not found", from)
		}

		sourceEnv := status.Environments[0]
		if len(sourceEnv.Services) == 0 {
			return fmt.Errorf("no services found in environment '%s'", from)
		}

		// Use the first service's last deployment
		sourceService := sourceEnv.Services[0]
		if sourceService.LastDeployment == nil {
			return fmt.Errorf("no deployments found in environment '%s'", from)
		}

		// 2. Get the full deployment details to extract git info
		var sourceDeploy *api.Deployment
		err = ui.RunWithSpinner("Fetching deployment details...", func() error {
			var fetchErr error
			sourceDeploy, fetchErr = client.GetDeployment(sourceService.LastDeployment.ID)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to get deployment: %w", err)
		}

		// 3. Deploy to target environment with the same git info
		deployReq := &api.DeployRequest{
			Environment: to,
		}
		if sourceDeploy.Meta != nil {
			deployReq.GitSha = sourceDeploy.Meta.GitSha
			deployReq.GitBranch = sourceDeploy.Meta.GitBranch
		}

		var deployment *api.Deployment
		err = ui.RunWithSpinner(
			fmt.Sprintf("Promoting %s → %s...", from, to),
			func() error {
				var promoteErr error
				deployment, promoteErr = client.Deploy(projectID, deployReq)
				return promoteErr
			},
		)
		if err != nil {
			return fmt.Errorf("promotion failed: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(deployment)
			return nil
		}

		fmt.Println()
		ui.PrintSuccess(fmt.Sprintf("Promoted %s → %s", from, to))
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
	promoteCmd.Flags().StringVar(&promoteFromFlag, "from", "staging", "Source environment")
	promoteCmd.Flags().StringVar(&promoteToFlag, "to", "production", "Target environment")
	rootCmd.AddCommand(promoteCmd)
}
