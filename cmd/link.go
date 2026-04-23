package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	flagLinkService string
	flagLinkEnv     string
)

var linkCmd = &cobra.Command{
	Use:   "link [project-id]",
	Short: "Link current directory to an existing project",
	Long: `Link the current directory to an existing Upuai Cloud project.

If no project ID is provided, shows a list of available projects to select from.
Use --service and --env flags to skip interactive prompts (useful in CI/automation).

Examples:
  upuai link
  upuai link <project-id>
  upuai link --service estilia-api
  upuai link <project-id> --env staging --service estilia-api`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		client := api.NewClient()
		var project *api.Project

		if len(args) == 1 {
			// Direct link by ID
			err := ui.RunWithSpinner("Fetching project...", func() error {
				var fetchErr error
				project, fetchErr = client.GetProject(args[0])
				return fetchErr
			})
			if err != nil {
				return fmt.Errorf("project not found: %w", err)
			}
		} else if flagLinkService != "" || flagLinkEnv != "" {
			// Non-interactive: use current project from config
			cfg, _ := config.LoadProjectConfig()
			if cfg == nil || cfg.ProjectID == "" {
				return fmt.Errorf("no project linked — run 'upuai init' or provide a project ID")
			}
			err := ui.RunWithSpinner("Fetching project...", func() error {
				var fetchErr error
				project, fetchErr = client.GetProject(cfg.ProjectID)
				return fetchErr
			})
			if err != nil {
				return fmt.Errorf("project not found: %w", err)
			}
		} else {
			// Interactive selection
			var projects []api.Project
			err := ui.RunWithSpinner("Loading projects...", func() error {
				var listErr error
				projects, listErr = client.ListProjects()
				return listErr
			})
			if err != nil {
				return fmt.Errorf("failed to list projects: %w", err)
			}

			if len(projects) == 0 {
				ui.PrintInfo("No projects found. Run 'upuai init' to create one.")
				return nil
			}

			names := make([]string, len(projects))
			for i, p := range projects {
				names[i] = fmt.Sprintf("%s (%s)", p.Name, p.ID)
			}

			selected, err := ui.SelectOne("Select a project:", names)
			if err != nil {
				return err
			}

			for i, name := range names {
				if name == selected {
					project = &projects[i]
					break
				}
			}
		}

		if project == nil {
			return fmt.Errorf("no project selected")
		}

		// Select environment
		var environments []api.Environment
		err := ui.RunWithSpinner("Loading environments...", func() error {
			var listErr error
			environments, listErr = client.ListEnvironments(project.ID)
			return listErr
		})
		if err != nil {
			return fmt.Errorf("failed to list environments: %w", err)
		}

		// Determine which environment name to use:
		// 1. --env flag (explicit), 2. current config env (when --service given), 3. interactive
		resolvedEnvName := flagLinkEnv
		if resolvedEnvName == "" && flagLinkService != "" {
			// When switching service non-interactively, default to the current config environment
			if currentCfg, _ := config.LoadProjectConfig(); currentCfg != nil && currentCfg.Environment != "" {
				resolvedEnvName = currentCfg.Environment
			}
		}

		var selectedEnv api.Environment
		if len(environments) == 0 {
			return fmt.Errorf("no environments found in project")
		} else if len(environments) == 1 {
			selectedEnv = environments[0]
		} else if resolvedEnvName != "" {
			// Non-interactive: match by env name
			for _, e := range environments {
				if strings.EqualFold(e.Name, resolvedEnvName) {
					selectedEnv = e
					break
				}
			}
			if selectedEnv.ID == "" {
				return fmt.Errorf("environment %q not found in project", resolvedEnvName)
			}
		} else {
			envNames := make([]string, len(environments))
			for i, e := range environments {
				envNames[i] = e.Name
			}

			envName, selErr := ui.SelectOne("Select environment:", envNames)
			if selErr != nil {
				return selErr
			}
			for _, e := range environments {
				if e.Name == envName {
					selectedEnv = e
					break
				}
			}
		}

		// Select service
		var services []api.AppService
		err = ui.RunWithSpinner("Loading services...", func() error {
			var listErr error
			services, listErr = client.ListServices(project.ID)
			return listErr
		})
		if err != nil {
			return fmt.Errorf("failed to list services: %w", err)
		}

		var serviceID, serviceName string
		if flagLinkService != "" {
			// Non-interactive: match by service name or ID
			for _, s := range services {
				if strings.EqualFold(s.Name, flagLinkService) || s.ID == flagLinkService {
					serviceID = s.ID
					serviceName = s.Name
					break
				}
			}
			if serviceID == "" {
				return fmt.Errorf("service %q not found in project", flagLinkService)
			}
		} else if len(services) == 1 {
			serviceID = services[0].ID
			serviceName = services[0].Name
		} else if len(services) > 1 {
			svcNames := make([]string, len(services))
			for i, s := range services {
				svcNames[i] = fmt.Sprintf("%s (%s)", s.Name, s.Type)
			}

			svcSelected, selErr := ui.SelectOne("Select service:", svcNames)
			if selErr != nil {
				return selErr
			}
			for i, name := range svcNames {
				if name == svcSelected {
					serviceID = services[i].ID
					serviceName = services[i].Name
					break
				}
			}
		}

		cfg := &config.ProjectConfig{
			ProjectID:     project.ID,
			ProjectName:   project.Name,
			ServiceID:     serviceID,
			ServiceName:   serviceName,
			EnvironmentID: selectedEnv.ID,
			Environment:   selectedEnv.Name,
		}
		if err := config.SaveProjectConfig(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Println()
		ui.PrintSuccess("Linked to project " + ui.Accent.Render(project.Name))
		pairs := []string{
			"ID", project.ID,
			"Environment", selectedEnv.Name,
		}
		if serviceName != "" {
			pairs = append(pairs, "Service", serviceName)
		}
		ui.PrintKeyValue(pairs...)
		fmt.Println()

		return nil
	},
}

func init() {
	linkCmd.Flags().StringVar(&flagLinkService, "service", "", "Service name or ID to link (skips interactive prompt)")
	linkCmd.Flags().StringVar(&flagLinkEnv, "env", "", "Environment name to link (skips interactive prompt)")
	rootCmd.AddCommand(linkCmd)
}
