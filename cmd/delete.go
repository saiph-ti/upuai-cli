package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete the linked project",
	Long:  `Delete the linked project. This action is irreversible and requires double confirmation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		client := api.NewClient()

		var project *api.Project
		err = ui.RunWithSpinner("Fetching project...", func() error {
			var fetchErr error
			project, fetchErr = client.GetProject(projectID)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to fetch project: %w", err)
		}

		if !flagYes {
			// First confirmation
			confirmed, err := ui.Confirm(fmt.Sprintf("Delete project %q? This cannot be undone.", project.Name))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Delete cancelled")
				return nil
			}

			// Second confirmation
			name, err := ui.InputText("Type the project name to confirm:", project.Name)
			if err != nil {
				return err
			}
			if name != project.Name {
				ui.PrintError("Project name does not match — delete cancelled")
				return nil
			}
		}

		err = ui.RunWithSpinner("Deleting project...", func() error {
			return client.DeleteProject(projectID)
		})
		if err != nil {
			return fmt.Errorf("failed to delete project: %w", err)
		}

		// Clean up local config
		cfg, _ := config.LoadProjectConfig()
		if cfg != nil && cfg.ProjectID == projectID {
			configPath := filepath.Join(".upuai", "config.json")
			_ = os.Remove(configPath)
		}

		ui.PrintSuccess(fmt.Sprintf("Project %s deleted", project.Name))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deleteCmd)
}
