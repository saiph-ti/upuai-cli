package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var templateCmd = &cobra.Command{
	Use:     "template",
	Aliases: []string{"templates"},
	Short:   "Manage database and service templates",
	Long: `List and deploy managed database templates (PostgreSQL, MySQL, Redis, MongoDB).

Examples:
  upuai template list
  upuai template deploy postgresql --name estilia-db
  upuai template deploy redis`,
}

var templateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available templates",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		client := api.NewClient()
		var templates []api.DatabaseTemplate
		err := ui.RunWithSpinner("Loading templates...", func() error {
			var fetchErr error
			templates, fetchErr = client.ListTemplates()
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list templates: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(templates)
			return nil
		}

		if len(templates) == 0 {
			ui.PrintInfo("No templates available")
			return nil
		}

		fmt.Println()
		table := ui.NewTable("Name", "Engine", "Version", "ID")
		for _, t := range templates {
			desc := t.Description
			if desc == "" {
				desc = t.Name
			}
			table.AddRow(desc, t.Engine, t.Version, t.ID)
		}
		table.Print()
		fmt.Println()

		return nil
	},
}

var (
	flagTemplateName string
	flagTemplateID   string
)

var templateDeployCmd = &cobra.Command{
	Use:   "deploy <engine>",
	Short: "Deploy a managed database template",
	Long: `Deploy a managed database. Pass the engine name as the first argument.

Supported engines: postgresql, mysql, redis, mongodb

Examples:
  upuai template deploy postgresql --name my-db
  upuai template deploy redis`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		cfg, _ := config.LoadProjectConfig()
		if cfg == nil || cfg.EnvironmentID == "" {
			return errNoServiceConfig
		}

		client := api.NewClient()

		// Resolve template ID
		templateID := flagTemplateID
		if templateID == "" {
			var templates []api.DatabaseTemplate
			err = ui.RunWithSpinner("Loading templates...", func() error {
				var fetchErr error
				templates, fetchErr = client.ListTemplates()
				return fetchErr
			})
			if err != nil {
				return fmt.Errorf("failed to list templates: %w", err)
			}

			if len(args) > 0 {
				// Match by engine name
				engine := strings.ToLower(args[0])
				var matches []api.DatabaseTemplate
				for _, t := range templates {
					if strings.ToLower(t.Engine) == engine || strings.EqualFold(t.Name, args[0]) {
						matches = append(matches, t)
					}
				}
				if len(matches) == 0 {
					return fmt.Errorf("no template found for engine %q — run 'upuai template list' to see available templates", args[0])
				}
				if len(matches) == 1 {
					templateID = matches[0].ID
				} else {
					// Multiple versions: let user pick
					names := make([]string, len(matches))
					for i, m := range matches {
						names[i] = fmt.Sprintf("%s %s (%s)", m.Engine, m.Version, m.ID)
					}
					selected, selErr := ui.SelectOne("Select template version:", names)
					if selErr != nil {
						return selErr
					}
					for i, n := range names {
						if n == selected {
							templateID = matches[i].ID
							break
						}
					}
				}
			} else {
				// No engine specified: show interactive picker
				names := make([]string, len(templates))
				for i, t := range templates {
					names[i] = fmt.Sprintf("%s %s (%s)", t.Name, t.Version, t.Engine)
				}
				selected, selErr := ui.SelectOne("Select template:", names)
				if selErr != nil {
					return selErr
				}
				for i, n := range names {
					if n == selected {
						templateID = templates[i].ID
						break
					}
				}
			}
		}

		if templateID == "" {
			return fmt.Errorf("no template selected")
		}

		// Deploy the template
		var result *api.DeployTemplateResponse
		err = ui.RunWithSpinner("Deploying database...", func() error {
			var deployErr error
			result, deployErr = client.DeployTemplate(projectID, &api.DeployTemplateRequest{
				TemplateID:    templateID,
				Name:          flagTemplateName,
				EnvironmentID: cfg.EnvironmentID,
			})
			return deployErr
		})
		if err != nil {
			return fmt.Errorf("failed to deploy template: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(result)
			return nil
		}

		fmt.Println()
		if len(result.Services) > 0 {
			svc := result.Services[0]
			ui.PrintSuccess(fmt.Sprintf("Database %s deploying", svc.Name))
			ui.PrintKeyValue(
				"Service ID", svc.ID,
				"Service Name", svc.Name,
				"Status", "deploying (provisioning in cluster)",
			)
			fmt.Println()

			// Fetch and display the connection string
			var vars []api.EnvVar
			varErr := ui.RunWithSpinner("Fetching connection details...", func() error {
				var fetchErr error
				vars, fetchErr = client.ListVariables(cfg.EnvironmentID, svc.ID)
				return fetchErr
			})
			if varErr == nil {
				connKeys := []string{"DATABASE_URL", "REDIS_URL", "MONGO_URL"}
				for _, v := range vars {
					for _, k := range connKeys {
						if v.Key == k {
							displayVal := v.DisplayValue()
							fmt.Println(ui.Bold.Render("Connection string:"))
							fmt.Printf("  %s=%s\n", v.Key, displayVal)
							fmt.Println()
							ui.PrintInfo(fmt.Sprintf("Set this on your app: upuai vars set \"%s=%s\"", v.Key, displayVal))
							fmt.Println()
						}
					}
				}
			}

			ui.PrintInfo("Run 'upuai status' to check when the database is ready")
		}

		return nil
	},
}

func init() {
	templateDeployCmd.Flags().StringVar(&flagTemplateName, "name", "", "Service name for the deployed database (optional, defaults to engine-version)")
	templateDeployCmd.Flags().StringVar(&flagTemplateID, "template-id", "", "Exact template ID (skips engine search)")
	templateCmd.AddCommand(templateListCmd)
	templateCmd.AddCommand(templateDeployCmd)
	rootCmd.AddCommand(templateCmd)
}
