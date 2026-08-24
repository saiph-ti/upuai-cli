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
project.

The cluster teardown runs in the background: the command returns as soon as the
request is accepted and the service shows as "Deleting" until it finishes.

The service can be restored from the project's deleted services for 30 days,
which brings back variables, domains and build config. Volumes are NOT restored:
their disks are erased on delete.

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
				"Delete service %q? This removes its deployments, buckets and domains, and erases its volumes for good. The service itself can be restored for 30 days.",
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

		err = ui.RunWithSpinner("Requesting deletion...", func() error {
			return client.DeleteService(projectID, target.ID)
		})
		if err != nil {
			// 409 = the API refused the request up front: a bucket service whose
			// MinIO bucket cannot be identified, or a database still referenced by
			// other services (re-run with --force there). The cluster teardown
			// itself runs on a queue and never answers this request.
			if apiErr, ok := err.(*api.APIError); ok && apiErr.StatusCode == 409 {
				return fmt.Errorf("deletion refused: %s", apiErr.Message)
			}
			return fmt.Errorf("failed to delete service: %w", err)
		}

		// If the deleted service was the one linked in .upuai/config.json, unlink
		// it so later commands don't target a ghost service.
		if cfg, _ := config.LoadProjectConfig(); cfg != nil && cfg.ServiceID == target.ID {
			_ = config.UpdateProjectConfig(func(c *config.ProjectConfig) {
				c.ServiceID = ""
				c.ServiceName = ""
			})
		}

		// A exclusão é assíncrona desde 2026-08-24: a API aceita o pedido (202) e o
		// teardown do cluster roda numa fila — num banco gerenciado ele leva
		// minutos. Dizer "deleted" aqui seria mentira; o estado real aparece na
		// interface (o serviço fica em "Excluindo") e em `upuai status`.
		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(map[string]interface{}{
				"deletionRequested": true,
				"serviceId":         target.ID,
				"name":              target.Name,
			})
			return nil
		}
		ui.PrintSuccess(fmt.Sprintf("Deletion of %s started — it keeps running in the background", target.Name))
		return nil
	},
}

func init() {
	serviceCmd.AddCommand(serviceDeleteCmd)
	rootCmd.AddCommand(serviceCmd)
}
