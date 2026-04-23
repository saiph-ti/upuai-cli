package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	rollbackListFlag bool
	rollbackToFlag   string
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback to a previous deployment",
	Long: `Rollback to a previous deployment.

Use --list to see recent deployments.
Use --to <deployment-id> to rollback to a specific deployment.
Without flags, rolls back to the previous deployment.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		if _, err := requireProject(); err != nil {
			return err
		}

		cfg, _ := config.LoadProjectConfig()
		if cfg == nil || cfg.EnvironmentID == "" || cfg.ServiceID == "" {
			return fmt.Errorf("project config missing environmentId or serviceId — run 'upuai link' to reconfigure")
		}

		client := api.NewClient()

		if rollbackListFlag {
			return listDeployments(client, cfg.EnvironmentID, cfg.ServiceID)
		}

		deployID := rollbackToFlag
		if deployID == "" {
			// Get previous deployment
			var deployments []api.Deployment
			err := ui.RunWithSpinner("Loading deployments...", func() error {
				var listErr error
				deployments, listErr = client.ListDeployments(cfg.EnvironmentID, cfg.ServiceID)
				return listErr
			})
			if err != nil {
				return fmt.Errorf("failed to list deployments: %w", err)
			}

			if len(deployments) < 2 {
				ui.PrintWarning("No previous deployment to rollback to")
				return nil
			}

			deployID = deployments[1].ID
		}

		if !flagYes {
			confirmed, err := ui.Confirm(fmt.Sprintf("Rollback to deployment %s?", deployID))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Rollback cancelled")
				return nil
			}
		}

		var deployment *api.Deployment
		err := ui.RunWithSpinner("Rolling back...", func() error {
			var rollbackErr error
			deployment, rollbackErr = client.Rollback(deployID)
			return rollbackErr
		})
		if err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(deployment)
			return nil
		}

		fmt.Println()
		ui.PrintSuccess("Rollback successful!")
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

func listDeployments(client *api.Client, envID, serviceID string) error {
	var deployments []api.Deployment
	err := ui.RunWithSpinner("Loading deployments...", func() error {
		var listErr error
		deployments, listErr = client.ListDeployments(envID, serviceID)
		return listErr
	})
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	format := getOutputFormat()
	if format == ui.FormatJSON {
		ui.PrintJSON(deployments)
		return nil
	}

	if len(deployments) == 0 {
		ui.PrintInfo("No deployments found")
		return nil
	}

	fmt.Println()
	table := ui.NewTable("ID", "Status", "Trigger", "Created", "Commit")
	for _, d := range deployments {
		commit := "—"
		if d.Meta != nil && d.Meta.GitSha != "" {
			commit = d.Meta.GitSha
			if len(commit) > 7 {
				commit = commit[:7]
			}
		}
		table.AddRow(d.ID, d.Status, d.Trigger, d.CreatedAt, commit)
	}
	table.Print()
	fmt.Println()

	return nil
}

func init() {
	rollbackCmd.Flags().BoolVar(&rollbackListFlag, "list", false, "List recent deployments")
	rollbackCmd.Flags().StringVar(&rollbackToFlag, "to", "", "Rollback to specific deployment ID")
	rootCmd.AddCommand(rollbackCmd)
}
