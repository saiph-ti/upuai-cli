package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/detect"
	"github.com/upuai-cloud/cli/internal/git"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	flagInitName      string
	flagInitFramework string
	flagInitRepo      string
	flagInitBranch    string
	flagInitRootDir   string
	flagInitImage     string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Upuai Cloud project",
	Long: `Initialize a new Upuai Cloud project in the current directory.

Auto-detects your framework and creates a project configuration.
If a project already exists, use 'upuai link' instead.

By default the new project's service has no source (type=empty) — connect it
later via the dashboard or 'upuai add --type github ...'. To create the service
with a source in a single step, pass --repo (github), or --image (docker image).

Examples:
  upuai init                                                # interactive
  upuai init --name my-app --framework Next.js --yes        # CLI-only, empty service
  upuai init --name my-app --repo gbmiranda/site --yes      # github service ready to deploy
  upuai init --name api --repo org/monorepo --root-dir apps/api --branch main --yes
  upuai init --name web --image nginx:1.27 --yes            # docker_image service`,
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

		// Mutually-exclusive source flags
		if flagInitRepo != "" && flagInitImage != "" {
			return fmt.Errorf("--repo and --image are mutually exclusive")
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
			} else if flagYes {
				// Strict non-interactive: prefer a clean error over a hung prompt.
				return fmt.Errorf("could not auto-detect framework; pass --framework <name> (e.g. \"Next.js\", \"Node.js\", \"Go\") or run without --yes")
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
			if flagYes {
				return fmt.Errorf("--name is required when --yes is set (e.g. --name my-app)")
			}
			projectName, err = ui.InputText("Project name", "my-project")
			if err != nil {
				return err
			}
		}
		if projectName == "" {
			return fmt.Errorf("project name is required")
		}

		// Resolve service source from flags. github requires repo to be in
		// canonical owner/repo form — reuse the helper from 'add' so both
		// commands share normalization (URLs / SSH / trailing .git).
		serviceType := "empty"
		var serviceSource *api.ServiceSourceConfig
		if flagInitRepo != "" {
			repo, parseErr := git.ParseRepo(flagInitRepo)
			if parseErr != nil {
				return fmt.Errorf("--repo: %w", parseErr)
			}
			branch := flagInitBranch
			if branch == "" {
				branch = "main"
			}
			serviceType = "github"
			serviceSource = &api.ServiceSourceConfig{
				Repo:          repo,
				Branch:        branch,
				RootDirectory: git.NormalizeRootDir(flagInitRootDir),
			}
		} else if flagInitImage != "" {
			serviceType = "docker_image"
			serviceSource = &api.ServiceSourceConfig{Image: flagInitImage}
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

		// Create the default service. When --repo/--image is set the service
		// is born deployable; otherwise it's a placeholder the user fills in
		// later (dashboard, 'upuai add', or another 'upuai init' on a sibling
		// service).
		serviceName := projectName
		var service *api.AppService
		err = ui.RunWithSpinner("Creating service...", func() error {
			var createErr error
			service, createErr = client.CreateService(project.ID, &api.CreateServiceRequest{
				Name:          serviceName,
				Type:          serviceType,
				EnvironmentID: envID,
				Source:        serviceSource,
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
		kv := []string{
			"Project", project.Name,
			"ID", project.ID,
			"Service", service.Name,
			"Type", service.Type,
		}
		if serviceSource != nil {
			if serviceSource.Repo != "" {
				kv = append(kv, "Repo", serviceSource.Repo+"@"+serviceSource.Branch)
				if serviceSource.RootDirectory != "" {
					kv = append(kv, "Root dir", serviceSource.RootDirectory)
				}
			}
			if serviceSource.Image != "" {
				kv = append(kv, "Image", serviceSource.Image)
			}
		}
		kv = append(kv, "Environment", env, "Framework", frameworkName)
		ui.PrintKeyValue(kv...)
		fmt.Println()
		if serviceType == "empty" {
			ui.PrintInfo("Next steps:")
			fmt.Println("  1. Attach a source: " + ui.Accent.Render("upuai add --type github --repo <owner>/<repo>") + " or use the dashboard")
			fmt.Println("  2. Re-link to the new service: " + ui.Accent.Render("upuai link "+project.ID+" --service <name>"))
			fmt.Println("  3. Deploy: " + ui.Accent.Render("upuai deploy --wait"))
		} else {
			ui.PrintInfo("Next steps:")
			fmt.Println("  1. Run " + ui.Accent.Render("upuai deploy --wait") + " to deploy your application")
			fmt.Println("  2. Run " + ui.Accent.Render("upuai status") + " to check project status")
		}
		fmt.Println()

		return nil
	},
}

func init() {
	initCmd.Flags().StringVar(&flagInitName, "name", "", "Project name (skips prompt)")
	initCmd.Flags().StringVar(&flagInitFramework, "framework", "", "Framework name (skips detection)")
	initCmd.Flags().StringVar(&flagInitRepo, "repo", "", "GitHub repo as 'owner/repo' (URLs aceitas e normalizadas) — creates a github-type service ready to deploy")
	initCmd.Flags().StringVar(&flagInitBranch, "branch", "", "Git branch (used with --repo, default: main)")
	initCmd.Flags().StringVar(&flagInitRootDir, "root-dir", "", "Root directory within the repo (for monorepos, e.g. apps/api)")
	initCmd.Flags().StringVar(&flagInitImage, "image", "", "Docker image to deploy (e.g. nginx:1.27) — creates a docker_image-type service; mutually exclusive with --repo")
	rootCmd.AddCommand(initCmd)
}
