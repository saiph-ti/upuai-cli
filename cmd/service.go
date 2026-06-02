package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage individual services",
	Long: `Operate on a single service within the current project.

Examples:
  upuai service delete api`,
}

var serviceDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Permanently delete a single service (keeps the project)",
	Long: `Delete one service and all of its resources — deployments, volumes, bucket
attachments, cluster workloads and domains — without touching the rest of the
project. Irreversible.

This is the per-service counterpart to 'upuai delete' (whole project) and
'upuai down' (stop the deployment but keep the service).

<name> matches a service by name, slug, or ID within the current project.

Examples:
  upuai service delete api
  upuai service delete worker --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		projectID, err := requireProject()
		if err != nil {
			return err
		}
		serviceRef := args[0]

		client := api.NewClient()

		// Resolve <name> → serviceID client-side (the API is ID-only). Match by
		// ID, name, or slug — same rule as resolveServiceContext, but project-
		// scoped: delete needs projectID+serviceID, not an environment.
		var services []api.AppService
		err = ui.RunWithSpinner("Resolving service...", func() error {
			var listErr error
			services, listErr = client.ListServices(projectID)
			return listErr
		})
		if err != nil {
			return fmt.Errorf("failed to list services: %w", err)
		}

		var target *api.AppService
		for i := range services {
			s := &services[i]
			if s.ID == serviceRef ||
				strings.EqualFold(s.Name, serviceRef) ||
				strings.EqualFold(s.Slug, serviceRef) {
				target = s
				break
			}
		}
		if target == nil {
			return fmt.Errorf("service %q not found in project — run 'upuai status' to see available services", serviceRef)
		}

		if !flagYes {
			ok, err := ui.Confirm(fmt.Sprintf(
				"Delete service %q permanently? This removes its deployments, volumes, buckets and domains and cannot be undone.",
				target.Name,
			))
			if err != nil {
				return err
			}
			if !ok {
				ui.PrintInfo("Cancelled")
				return nil
			}
		}

		err = ui.RunWithSpinner("Deleting service...", func() error {
			return client.DeleteService(projectID, target.ID)
		})
		if err != nil {
			// 409 = the orchestrator could not tear down cluster resources for at
			// least one environment; the API intentionally keeps the service to
			// avoid drift (no half-deleted state). Surface that clearly so the user
			// retries rather than assuming a silent failure.
			if apiErr, ok := err.(*api.APIError); ok && apiErr.StatusCode == 409 {
				return fmt.Errorf("service not deleted — cluster cleanup failed: %s\n  retry once the cluster is reachable", apiErr.Message)
			}
			return fmt.Errorf("failed to delete service: %w", err)
		}

		// If the deleted service was the one linked in .upuai/config.json, unlink
		// it so later commands don't target a ghost service.
		if cfg, _ := config.LoadProjectConfig(); cfg != nil && cfg.ServiceID == target.ID {
			cfg.ServiceID = ""
			cfg.ServiceName = ""
			_ = config.SaveProjectConfig(cfg)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(map[string]interface{}{"deleted": true, "serviceId": target.ID, "name": target.Name})
			return nil
		}
		ui.PrintSuccess(fmt.Sprintf("Service %s deleted", target.Name))
		return nil
	},
}

func init() {
	serviceCmd.AddCommand(serviceDeleteCmd)
	rootCmd.AddCommand(serviceCmd)
}
