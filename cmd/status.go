package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show project status and services",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		client := api.NewClient()
		env := getEnvironment()

		var status *api.ProjectStatus
		err = ui.RunWithSpinner("Loading status...", func() error {
			var fetchErr error
			status, fetchErr = client.GetProjectStatus(projectID, env)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to get status: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(status)
			return nil
		}

		fmt.Println()
		ui.PrintKeyValue(
			"Project", status.Project.Name,
			"ID", status.Project.ID,
			"Status", status.Project.Status,
		)
		fmt.Println()

		hasServices := false
		for _, env := range status.Environments {
			if len(env.Services) > 0 {
				hasServices = true
				fmt.Println(ui.Bold.Render("Environment: " + env.Name))
				fmt.Println()

				table := ui.NewTable("Name", "Type", "Status", "URL", "Last Deploy")
				for _, svc := range env.Services {
					instanceStatus := formatServiceStatus(svc.Instance.Status)
					url := svc.Instance.URL
					if url == "" {
						url = "—"
					}
					lastDeploy := "—"
					if svc.LastDeployment != nil {
						lastDeploy = fmt.Sprintf("%s (%s)", svc.LastDeployment.Status, svc.LastDeployment.CreatedAt)
					}
					table.AddRow(svc.Name, svc.Type, instanceStatus, url, lastDeploy)
				}
				table.Print()
				fmt.Println()
			}
		}

		if !hasServices {
			ui.PrintInfo("No services deployed yet")
			ui.PrintInfo("Run 'upuai deploy' to deploy your application")
		}

		return nil
	},
}

// Mirrors the web's status vocabulary (upuai-web apps/web/src/lib/deploy-status.ts):
// green = healthy, red = broken, yellow = in flight, muted = at rest/unknown.
// Every state the API can emit is listed — anything falling through to `default`
// used to render muted grey, so a crashed service looked as calm as a stopped one.
func formatServiceStatus(status string) string {
	switch status {
	case "running", "active", "healthy", "success":
		return ui.StatusRunning.Render("● " + status)
	case "failed", "build_failed", "crashed", "error":
		return ui.StatusStopped.Render("● " + status)
	case "queued", "initializing", "building", "releasing", "deploying", "candidate_ready", "pending":
		return ui.StatusBuilding.Render("● " + status)
	default:
		// At rest (stopped, cancelled, superseded) and anything unrecognized.
		// `stopped` is deliberate — the platform scaled it to zero, it is not broken.
		return ui.Muted.Render("● " + status)
	}
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
