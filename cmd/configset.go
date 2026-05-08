package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/git"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	flagConfigBuilder            string
	flagConfigDockerfilePath     string
	flagConfigBuildCommand       string
	flagConfigStartCommand       string
	flagConfigHealthCheck        string
	flagConfigHealthCheckTimeout int
	flagConfigRootDir            string
)

var configSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Update the service instance configuration",
	Long: `Update build and deploy configuration for the linked service instance.

Examples:
  upuai config set --builder dockerfile --dockerfile-path apps/api/Dockerfile
  upuai config set --build-command "pnpm install && pnpm build" --start-command "node dist/server.js"
  upuai config set --root-dir apps/web --health-check /health`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		cfg, _ := config.LoadProjectConfig()
		if cfg == nil || cfg.EnvironmentID == "" || cfg.ServiceID == "" {
			return errNoServiceConfig
		}

		// Build the update request from provided flags
		hasSource := flagConfigRootDir != ""
		hasBuild := flagConfigBuilder != "" || flagConfigDockerfilePath != "" || flagConfigBuildCommand != ""
		hasDeploy := flagConfigStartCommand != "" || flagConfigHealthCheck != "" || flagConfigHealthCheckTimeout > 0

		if !hasSource && !hasBuild && !hasDeploy {
			return fmt.Errorf("no configuration flags provided — use --builder, --build-command, --start-command, --health-check, or --root-dir")
		}

		req := &api.UpdateInstanceRequest{}
		if hasSource {
			req.Source = &api.InstanceSourceConfig{RootDirectory: git.NormalizeRootDir(flagConfigRootDir)}
		}
		if hasBuild {
			req.Build = &api.InstanceBuildConfig{
				Builder:        flagConfigBuilder,
				DockerfilePath: flagConfigDockerfilePath,
				BuildCommand:   flagConfigBuildCommand,
			}
		}
		if hasDeploy {
			req.Deploy = &api.InstanceDeployConfig{
				StartCommand:       flagConfigStartCommand,
				HealthCheckPath:    flagConfigHealthCheck,
				HealthCheckTimeout: flagConfigHealthCheckTimeout,
			}
		}

		client := api.NewClient()

		err := ui.RunWithSpinner("Updating configuration...", func() error {
			return client.UpdateInstance(cfg.EnvironmentID, cfg.ServiceID, req)
		})
		if err != nil {
			return fmt.Errorf("failed to update config: %w", err)
		}

		fmt.Println()
		ui.PrintSuccess("Service configuration updated")
		fmt.Println()

		return nil
	},
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage service configuration",
}

var configShowCmd = &cobra.Command{
	Use:     "show",
	Aliases: []string{"get"},
	Short:   "Show current build/deploy configuration of the linked service",
	Long: `Show the build/deploy configuration of the linked service instance.

Reveals the current builder (railpack/dockerfile), build/start commands,
health check, and root directory — useful to confirm what 'config set' applied.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		envID, serviceID, err := requireServiceConfig()
		if err != nil {
			return err
		}

		client := api.NewClient()
		var inst *api.Instance
		err = ui.RunWithSpinner("Fetching configuration...", func() error {
			var ferr error
			inst, ferr = client.GetInstance(envID, serviceID)
			return ferr
		})
		if err != nil {
			return fmt.Errorf("failed to fetch config: %w", err)
		}

		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(inst)
			return nil
		}

		fmt.Println()
		ui.PrintKeyValue(
			"Service", inst.Name,
			"Type", inst.Type,
			"Status", inst.Status,
		)

		builder := "railpack (default)"
		dockerfilePath := "—"
		buildCommand := "—"
		rootDir := "—"
		startCommand := "—"
		healthCheck := "—"
		if inst.Config != nil {
			if b := inst.Config.Build; b != nil {
				if b.Builder != "" {
					builder = b.Builder
				}
				if b.DockerfilePath != "" {
					dockerfilePath = b.DockerfilePath
				}
				if b.BuildCommand != "" {
					buildCommand = b.BuildCommand
				}
			}
			if s := inst.Config.Source; s != nil && s.RootDirectory != "" {
				rootDir = s.RootDirectory
			}
			if d := inst.Config.Deploy; d != nil {
				if d.StartCommand != "" {
					startCommand = d.StartCommand
				}
				if d.HealthCheckPath != "" {
					healthCheck = d.HealthCheckPath
				}
			}
		}

		fmt.Println()
		ui.PrintKeyValue(
			"Builder", builder,
			"Dockerfile path", dockerfilePath,
			"Build command", buildCommand,
			"Root directory", rootDir,
			"Start command", startCommand,
			"Health check", healthCheck,
		)
		fmt.Println()
		ui.PrintInfo("Edit with: " + ui.Accent.Render("upuai config set --builder <railpack|dockerfile> ..."))
		fmt.Println()
		return nil
	},
}

func init() {
	configSetCmd.Flags().StringVar(&flagConfigRootDir, "root-dir", "", "Root directory within the repo (for monorepos)")
	configSetCmd.Flags().StringVar(&flagConfigBuilder, "builder", "", "Build system: dockerfile or railpack")
	configSetCmd.Flags().StringVar(&flagConfigDockerfilePath, "dockerfile-path", "", "Path to Dockerfile (used with --builder dockerfile)")
	configSetCmd.Flags().StringVar(&flagConfigBuildCommand, "build-command", "", "Command to build the service")
	configSetCmd.Flags().StringVar(&flagConfigStartCommand, "start-command", "", "Command to start the service")
	configSetCmd.Flags().StringVar(&flagConfigHealthCheck, "health-check", "", "HTTP path for health check (e.g. /health)")
	configSetCmd.Flags().IntVar(&flagConfigHealthCheckTimeout, "health-check-timeout", 0, "Initial delay in seconds before health checks start (default 5)")
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configShowCmd)
	rootCmd.AddCommand(configCmd)
}
