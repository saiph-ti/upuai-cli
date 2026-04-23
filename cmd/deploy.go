package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
	"github.com/upuai-cloud/cli/internal/watcher"
)

var deployWatchFlag bool

var deployCmd = &cobra.Command{
	Use:     "deploy",
	Aliases: []string{"up"},
	Short:   "Deploy the current project",
	Long: `Deploy the current project to Upuai Cloud.

Triggers a new deployment for the linked project.
Use --watch for auto-redeploy on file changes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		env := getEnvironment()

		cfg, _ := config.LoadProjectConfig()
		serviceID := ""
		if cfg != nil {
			serviceID = cfg.ServiceID
		}

		if err := runDeploy(projectID, env, serviceID); err != nil {
			return err
		}

		if deployWatchFlag {
			return runWatchMode(projectID, env, serviceID)
		}

		return nil
	},
}

func runDeploy(projectID, env, serviceID string) error {
	client := api.NewClient()
	var deployment *api.Deployment

	err := ui.RunWithSpinner("Deploying to "+env+"...", func() error {
		var deployErr error
		deployment, deployErr = client.Deploy(projectID, &api.DeployRequest{
			Environment: env,
			ServiceID:   serviceID,
		})
		return deployErr
	})
	if err != nil {
		return fmt.Errorf("deployment failed: %w", err)
	}

	format := getOutputFormat()
	if format == ui.FormatJSON {
		ui.PrintJSON(deployment)
		return nil
	}

	fmt.Println()
	ui.PrintSuccess("Deployment triggered!")
	fmt.Println()
	ui.PrintKeyValue(
		"Deployment", deployment.ID,
		"Status", deployment.Status,
	)
	if deployment.URL != "" {
		ui.PrintKeyValue("URL", deployment.URL)
	}
	fmt.Println()

	return nil
}

func runWatchMode(projectID, env, serviceID string) error {
	fmt.Println()
	ui.PrintInfo("Watching for file changes... (press Ctrl+C to stop)")
	fmt.Println()

	deployCount := 0
	return watcher.Watch(".", 500*time.Millisecond, func() error {
		deployCount++
		fmt.Printf("\n%s File change detected (deploy #%d)\n",
			ui.Info.Render("→"), deployCount)
		return runDeploy(projectID, env, serviceID)
	})
}

func init() {
	deployCmd.Flags().BoolVarP(&deployWatchFlag, "watch", "w", false, "Watch for changes and auto-redeploy")
	rootCmd.AddCommand(deployCmd)
}
