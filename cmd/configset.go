package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
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
			req.Source = &api.InstanceSourceConfig{RootDirectory: flagConfigRootDir}
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

func init() {
	configSetCmd.Flags().StringVar(&flagConfigRootDir, "root-dir", "", "Root directory within the repo (for monorepos)")
	configSetCmd.Flags().StringVar(&flagConfigBuilder, "builder", "", "Build system: dockerfile or railpack")
	configSetCmd.Flags().StringVar(&flagConfigDockerfilePath, "dockerfile-path", "", "Path to Dockerfile (used with --builder dockerfile)")
	configSetCmd.Flags().StringVar(&flagConfigBuildCommand, "build-command", "", "Command to build the service")
	configSetCmd.Flags().StringVar(&flagConfigStartCommand, "start-command", "", "Command to start the service")
	configSetCmd.Flags().StringVar(&flagConfigHealthCheck, "health-check", "", "HTTP path for health check (e.g. /health)")
	configSetCmd.Flags().IntVar(&flagConfigHealthCheckTimeout, "health-check-timeout", 0, "Initial delay in seconds before health checks start (default 5)")
	configCmd.AddCommand(configSetCmd)
	rootCmd.AddCommand(configCmd)
}
