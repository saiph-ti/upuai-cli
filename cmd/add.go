package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/git"
	"github.com/upuai-cloud/cli/internal/ui"
)

// serviceTypeLabels are the user-facing names shown in the interactive picker.
// serviceTypeValues are the corresponding API values (must match API enum).
var serviceTypeLabels = []string{"app", "bucket", "database", "docker", "docker image", "function", "github", "gitlab"}
var serviceTypeAPIValues = map[string]string{
	"app":          "empty",
	"bucket":       "bucket",
	"database":     "database",
	"docker":       "docker",
	"docker image": "docker_image",
	"function":     "function",
	"github":       "github",
	"gitlab":       "gitlab",
}

// defaultBucketRegion mirrors the value the web UI sends today
// (apps/web/.../bucket-picker-dialog.tsx). MinIO behind the orchestrator
// is single-region; this is a label, not a routing knob.
const defaultBucketRegion = "us-east-1"

var (
	flagAddType               string
	flagAddName               string
	flagAddImage              string
	flagAddRepo               string
	flagAddBranch             string
	flagAddRootDir            string
	flagAddBuilder            string
	flagAddDockerfilePath     string
	flagAddStartCommand       string
	flagAddHealthCheck        string
	flagAddHealthCheckTimeout int
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new service to the project",
	Long: `Add a new service to the project via interactive wizard.

Examples:
  upuai add
  upuai add --type database --name postgres
  upuai add --type github --name api --repo org/repo --branch main
  upuai add --type github --name api --repo https://github.com/org/monorepo --root-dir apps/api --builder dockerfile --dockerfile-path apps/api/Dockerfile`,
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

		// Select service type (skip picker if --image or --repo implies the type)
		serviceTypeLabel := flagAddType
		if serviceTypeLabel == "" && flagAddImage == "" && flagAddRepo == "" {
			serviceTypeLabel, err = ui.SelectOne("Service type:", serviceTypeLabels)
			if err != nil {
				return err
			}
		}
		serviceType := "empty"
		if serviceTypeLabel != "" {
			var ok bool
			serviceType, ok = serviceTypeAPIValues[serviceTypeLabel]
			if !ok {
				return fmt.Errorf(
					"invalid service type %q\n  valid types: %v\n  if you expected %q to work, run 'upuai upgrade' — it was added in v0.4.0\n  docs: https://upuai.com.br/docs/upuai-cli",
					serviceTypeLabel, serviceTypeLabels, serviceTypeLabel,
				)
			}
		}

		// Enter service name
		name := flagAddName
		if name == "" {
			name, err = ui.InputText("Service name:", "my-service")
			if err != nil {
				return err
			}
		}

		if name == "" {
			return fmt.Errorf("service name is required")
		}

		client := api.NewClient()

		// Bucket has its own provisioning endpoint that creates the underlying
		// MinIO bucket + credentials in addition to the canvas service node.
		// Source/builder/start-command flags are not meaningful here.
		if serviceType == "bucket" {
			var bucketResp *api.CreateBucketAsServiceResponse
			err = ui.RunWithSpinner("Creating bucket...", func() error {
				var createErr error
				bucketResp, createErr = client.CreateBucketAsService(projectID, &api.CreateBucketAsServiceRequest{
					Name:          name,
					Region:        defaultBucketRegion,
					EnvironmentID: cfg.EnvironmentID,
				})
				return createErr
			})
			if err != nil {
				return fmt.Errorf("failed to create bucket: %w", err)
			}

			format := getOutputFormat()
			if format == ui.FormatJSON {
				ui.PrintJSON(bucketResp)
				return nil
			}

			fmt.Println()
			ui.PrintSuccess(fmt.Sprintf("Bucket %s created", bucketResp.Name))
			ui.PrintKeyValue(
				"Service ID", bucketResp.ServiceID,
				"Bucket ID", bucketResp.BucketID,
				"Name", bucketResp.Name,
				"Region", bucketResp.Region,
			)

			if cfg.ServiceID == "" {
				cfg.ServiceID = bucketResp.ServiceID
				cfg.ServiceName = bucketResp.Name
				_ = config.SaveProjectConfig(cfg)
				fmt.Println()
				ui.PrintInfo("Linked to new bucket")
			}

			fmt.Println()
			return nil
		}

		// Resolve source and override type if --image or --repo provided.
		// --repo is normalized to "owner/repo" before sending — the API rejects
		// URLs and the UI dialog assumes that exact format. See runbook
		// 2026-05-08-cli-source-normalization.md.
		var source *api.ServiceSourceConfig
		if flagAddImage != "" {
			serviceType = "docker_image"
			source = &api.ServiceSourceConfig{Image: flagAddImage}
		} else if flagAddRepo != "" {
			repo, parseErr := git.ParseRepo(flagAddRepo)
			if parseErr != nil {
				return fmt.Errorf("--repo: %w", parseErr)
			}
			serviceType = "github"
			branch := flagAddBranch
			if branch == "" {
				branch = "main"
			}
			source = &api.ServiceSourceConfig{
				Repo:          repo,
				Branch:        branch,
				RootDirectory: git.NormalizeRootDir(flagAddRootDir),
			}
		} else if flagAddRootDir != "" {
			source = &api.ServiceSourceConfig{RootDirectory: git.NormalizeRootDir(flagAddRootDir)}
		}

		var service *api.AppService
		err = ui.RunWithSpinner("Creating service...", func() error {
			var createErr error
			service, createErr = client.CreateService(projectID, &api.CreateServiceRequest{
				Name:          name,
				Type:          serviceType,
				EnvironmentID: cfg.EnvironmentID,
				Source:        source,
			})
			return createErr
		})
		if err != nil {
			return fmt.Errorf("failed to create service: %w", err)
		}

		// Apply build/deploy config if any flags were provided
		hasBuildConfig := flagAddBuilder != "" || flagAddDockerfilePath != ""
		hasDeployConfig := flagAddStartCommand != "" || flagAddHealthCheck != "" || flagAddHealthCheckTimeout > 0
		if hasBuildConfig || hasDeployConfig {
			req := &api.UpdateInstanceRequest{}
			if hasBuildConfig {
				req.Build = &api.InstanceBuildConfig{
					Builder:        flagAddBuilder,
					DockerfilePath: flagAddDockerfilePath,
				}
			}
			if hasDeployConfig {
				req.Deploy = &api.InstanceDeployConfig{
					StartCommand:       flagAddStartCommand,
					HealthCheckPath:    flagAddHealthCheck,
					HealthCheckTimeout: flagAddHealthCheckTimeout,
				}
			}
			err = ui.RunWithSpinner("Configuring service...", func() error {
				return client.UpdateInstance(cfg.EnvironmentID, service.ID, req)
			})
			if err != nil {
				ui.PrintWarning(fmt.Sprintf("Service created but config update failed: %v", err))
			}
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(service)
			return nil
		}

		fmt.Println()
		ui.PrintSuccess(fmt.Sprintf("Service %s created", service.Name))
		ui.PrintKeyValue(
			"ID", service.ID,
			"Name", service.Name,
			"Type", service.Type,
		)

		// Update local config if no service was linked
		if cfg.ServiceID == "" {
			cfg.ServiceID = service.ID
			cfg.ServiceName = service.Name
			_ = config.SaveProjectConfig(cfg)
			fmt.Println()
			ui.PrintInfo("Linked to new service")
		}

		fmt.Println()
		return nil
	},
}

func init() {
	addCmd.Flags().StringVar(&flagAddType, "type", "", "Service type: app, bucket, database, docker, docker image, function, github, gitlab")
	addCmd.Flags().StringVar(&flagAddName, "name", "", "Service name (skips prompt)")
	addCmd.Flags().StringVar(&flagAddImage, "image", "", "Docker image to deploy (e.g. nginx:latest) — sets type to docker_image")
	addCmd.Flags().StringVar(&flagAddRepo, "repo", "", "GitHub repo as 'owner/repo' (URLs aceitas e normalizadas) — sets type to github")
	addCmd.Flags().StringVar(&flagAddBranch, "branch", "main", "Git branch (used with --repo, default: main)")
	addCmd.Flags().StringVar(&flagAddRootDir, "root-dir", "", "Root directory within the repo (for monorepos, e.g. apps/api)")
	addCmd.Flags().StringVar(&flagAddBuilder, "builder", "", "Build system: dockerfile or railpack")
	addCmd.Flags().StringVar(&flagAddDockerfilePath, "dockerfile-path", "", "Path to Dockerfile (used with --builder dockerfile)")
	addCmd.Flags().StringVar(&flagAddStartCommand, "start-command", "", "Command to start the service")
	addCmd.Flags().StringVar(&flagAddHealthCheck, "health-check", "", "HTTP path for health check (e.g. /health)")
	addCmd.Flags().IntVar(&flagAddHealthCheckTimeout, "health-check-timeout", 0, "Initial delay in seconds before health checks start (default 5)")
	rootCmd.AddCommand(addCmd)
}
