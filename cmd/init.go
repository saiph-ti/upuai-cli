package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/detect"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	flagInitName      string
	flagInitFramework string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Upuai Cloud project",
	Long: `Initialize a new Upuai Cloud project in the current directory.

Auto-detects your framework and creates a project configuration.
If a project already exists, use 'upuai link' instead.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		if config.ProjectConfigExists() {
			cfg, _ := config.LoadProjectConfig()
			if cfg != nil && cfg.ProjectID != "" {
				ui.PrintWarning(fmt.Sprintf("Project already initialized: %s", cfg.ProjectName))
				ui.PrintInfo("Use 'upuai link' to link to a different project")
				return nil
			}
		}

		ui.PrintBanner()

		var err error

		// Auto-detect framework
		frameworkName := flagInitFramework
		if frameworkName == "" {
			result := detect.DetectFramework(".")
			if result.Matched {
				frameworkName = result.Framework.Name
				ui.PrintSuccess("Detected framework: " + ui.Accent.Render(frameworkName))
			} else {
				ui.PrintWarning("Could not auto-detect framework")
				detected := detect.ListDetectedFrameworks(".")
				if len(detected) > 0 {
					names := make([]string, len(detected))
					for i, fw := range detected {
						names[i] = fw.Name
					}
					frameworkName, err = ui.SelectOne("Select your framework:", names)
					if err != nil {
						return err
					}
				} else {
					frameworkName, err = ui.InputText("Framework name", "Node.js")
					if err != nil {
						return err
					}
				}
			}
		}

		// Get project name
		projectName := flagInitName
		if projectName == "" {
			projectName, err = ui.InputText("Project name", "my-project")
			if err != nil {
				return err
			}
		}
		if projectName == "" {
			return fmt.Errorf("project name is required")
		}

		// Create project on API
		client := api.NewClient()
		var project *api.Project

		err = ui.RunWithSpinner("Creating project...", func() error {
			var createErr error
			project, createErr = client.CreateProject(&api.CreateProjectRequest{
				Name: projectName,
			})
			return createErr
		})
		if err != nil {
			return fmt.Errorf("failed to create project: %w", err)
		}

		// Select environment
		env := getEnvironment()
		if !flagYes {
			env, err = ui.SelectOne("Default environment:", []string{"production", "staging", "development"})
			if err != nil {
				return err
			}
		}

		// Fetch environments from the project to find the matching one
		var environments []api.Environment
		err = ui.RunWithSpinner("Setting up environment...", func() error {
			var listErr error
			environments, listErr = client.ListEnvironments(project.ID)
			return listErr
		})
		if err != nil {
			return fmt.Errorf("failed to list environments: %w", err)
		}

		// Find or create the selected environment
		var envID string
		for _, e := range environments {
			if e.Name == env {
				envID = e.ID
				break
			}
		}
		if envID == "" {
			var newEnv *api.Environment
			err = ui.RunWithSpinner("Creating environment...", func() error {
				var createErr error
				newEnv, createErr = client.CreateEnvironment(project.ID, &api.CreateEnvironmentRequest{
					Name: env,
				})
				return createErr
			})
			if err != nil {
				return fmt.Errorf("failed to create environment: %w", err)
			}
			envID = newEnv.ID
		}

		// Create a default service
		serviceName := projectName
		var service *api.AppService
		err = ui.RunWithSpinner("Creating service...", func() error {
			var createErr error
			service, createErr = client.CreateService(project.ID, &api.CreateServiceRequest{
				Name:          serviceName,
				Type:          "empty",
				EnvironmentID: envID,
			})
			return createErr
		})
		if err != nil {
			return fmt.Errorf("failed to create service: %w", err)
		}

		// Save local config
		projectCfg := &config.ProjectConfig{
			ProjectID:     project.ID,
			ProjectName:   project.Name,
			ServiceID:     service.ID,
			ServiceName:   service.Name,
			EnvironmentID: envID,
			Environment:   env,
			Framework:     frameworkName,
		}
		if err := config.SaveProjectConfig(projectCfg); err != nil {
			return fmt.Errorf("failed to save project config: %w", err)
		}

		fmt.Println()
		ui.PrintSuccess("Project initialized!")
		fmt.Println()
		ui.PrintKeyValue(
			"Project", project.Name,
			"ID", project.ID,
			"Service", service.Name,
			"Environment", env,
			"Framework", frameworkName,
		)
		fmt.Println()
		ui.PrintInfo("Next steps:")
		fmt.Println("  1. Run " + ui.Accent.Render("upuai deploy") + " to deploy your application")
		fmt.Println("  2. Run " + ui.Accent.Render("upuai status") + " to check project status")
		fmt.Println()

		return nil
	},
}

func init() {
	initCmd.Flags().StringVar(&flagInitName, "name", "", "Project name (skips prompt)")
	initCmd.Flags().StringVar(&flagInitFramework, "framework", "", "Framework name (skips detection)")
	rootCmd.AddCommand(initCmd)
}
