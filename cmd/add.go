package cmd

import (
	"fmt"
	"strings"

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
	flagAddEngine             string
	flagAddImage              string
	flagAddRepo               string
	flagAddBranch             string
	flagAddRootDir            string
	flagAddBuilder            string
	flagAddDockerfilePath     string
	flagAddStartCommand       string
	flagAddHealthCheck        string
	flagAddHealthCheckTimeout int
	flagAddRegistryUser       string
	flagAddRegistryPassword   string
	flagAddRegistryHost       string
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

		// Managed database/cache (Postgres, Redis, MySQL, Mongo) provisiona via
		// template gerenciado — cria o serviço + injeta as connection vars
		// (DATABASE_URL/REDIS_URL/...) automaticamente. O caminho genérico
		// CreateService(type=database) criava um nó "vazio" sem provisionamento
		// nem variáveis, o que confundia (relato de integração Rails). Mesmo
		// padrão do bucket acima: tipo com provisionamento dedicado.
		if serviceType == "database" {
			return runManagedDatabaseAdd(projectID, cfg, flagAddName, flagAddEngine)
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
			// Credencial de registry privado é opcional, mas se vier a password,
			// o user também precisa (paridade com o magic env-var da plataforma).
			if (flagAddRegistryUser != "") != (flagAddRegistryPassword != "") {
				return fmt.Errorf("--registry-user e --registry-password devem ser usados juntos")
			}
		} else if flagAddRepo != "" {
			repo, detected, parseErr := git.ParseRepoWithProvider(flagAddRepo)
			if parseErr != nil {
				return fmt.Errorf("--repo: %w", parseErr)
			}
			// --type, quando dado, precisa ser github|gitlab pra combinar com --repo.
			explicit := ""
			if flagAddType != "" {
				if serviceType != "github" && serviceType != "gitlab" {
					return fmt.Errorf("--type %s não combina com --repo (use --type github|gitlab ou omita)", flagAddType)
				}
				explicit = serviceType
			}
			provider, resolveErr := git.ResolveProvider(explicit, detected)
			if resolveErr != nil {
				return fmt.Errorf("--repo: %w", resolveErr)
			}
			serviceType = provider
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

		// Credencial de registry privado (imagem privada): grava as env vars
		// mágicas que a plataforma consome (DOCKER_REGISTRY_*) e marca a senha
		// como secret. A API as converte em imagePullSecret e as remove do
		// runtime do container (não vazam). Paridade Railway.
		if flagAddImage != "" && flagAddRegistryUser != "" && flagAddRegistryPassword != "" {
			vars := []api.VariableInput{
				{Key: "DOCKER_REGISTRY_USERNAME", Value: flagAddRegistryUser},
				{Key: "DOCKER_REGISTRY_PASSWORD", Value: flagAddRegistryPassword, IsSecret: true},
			}
			if flagAddRegistryHost != "" {
				vars = append(vars, api.VariableInput{Key: "DOCKER_REGISTRY_HOST", Value: flagAddRegistryHost})
			}
			err = ui.RunWithSpinner("Setting registry credentials...", func() error {
				_, setErr := client.SetVariables(cfg.EnvironmentID, service.ID, vars)
				return setErr
			})
			if err != nil {
				ui.PrintWarning(fmt.Sprintf("Service created but registry credentials failed: %v", err))
			}
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

// runManagedDatabaseAdd provisiona um banco/cache gerenciado via template
// (Postgres, Redis, MySQL, Mongo). Espelha o fluxo do bucket: o template cria
// o ServiceInstance + injeta as connection vars (DATABASE_URL/REDIS_URL/...) no
// momento da criação. Seleção do engine: flag --engine (match por engine ou
// nome) ou picker interativo. Sem nome → o template usa o default
// "<engine>-<versão>".
func runManagedDatabaseAdd(projectID string, cfg *config.ProjectConfig, name, engine string) error {
	client := api.NewClient()

	var templates []api.DatabaseTemplate
	err := ui.RunWithSpinner("Loading managed database templates...", func() error {
		var listErr error
		templates, listErr = client.ListTemplates()
		return listErr
	})
	if err != nil {
		return fmt.Errorf("failed to list templates: %w", err)
	}
	if len(templates) == 0 {
		return fmt.Errorf("no managed database templates available")
	}

	chosen, err := pickDatabaseTemplate(templates, engine)
	if err != nil {
		return err
	}

	req := &api.DeployTemplateRequest{
		TemplateID:    chosen.ID,
		Name:          name,
		EnvironmentID: cfg.EnvironmentID,
	}
	var resp *api.DeployTemplateResponse
	err = ui.RunWithSpinner(fmt.Sprintf("Provisioning managed %s...", chosen.Engine), func() error {
		var deployErr error
		resp, deployErr = client.DeployTemplate(projectID, req)
		return deployErr
	})
	if err != nil {
		return fmt.Errorf("failed to provision %s: %w", chosen.Engine, err)
	}

	format := getOutputFormat()
	if format == ui.FormatJSON {
		ui.PrintJSON(resp)
		return nil
	}

	fmt.Println()
	ui.PrintSuccess(fmt.Sprintf("Managed %s %s provisioned", chosen.Engine, chosen.Version))
	for _, s := range resp.Services {
		ui.PrintKeyValue("Service ID", s.ID, "Name", s.Name)
	}
	ui.PrintInfo("Connection variables (DATABASE_URL/REDIS_URL/...) are injected automatically — reference them from your app service.")

	// Liga o config local ao primeiro serviço criado se nada estava linkado.
	if cfg.ServiceID == "" && len(resp.Services) > 0 {
		cfg.ServiceID = resp.Services[0].ID
		cfg.ServiceName = resp.Services[0].Name
		_ = config.SaveProjectConfig(cfg)
		fmt.Println()
		ui.PrintInfo("Linked to new managed database")
	}

	fmt.Println()
	return nil
}

// pickDatabaseTemplate resolve o template escolhido: por --engine (match em
// engine ou nome, case-insensitive) ou via picker interativo quando o flag é
// vazio.
func pickDatabaseTemplate(templates []api.DatabaseTemplate, engine string) (*api.DatabaseTemplate, error) {
	if engine != "" {
		for i := range templates {
			if strings.EqualFold(templates[i].Engine, engine) || strings.EqualFold(templates[i].Name, engine) {
				return &templates[i], nil
			}
		}
		available := make([]string, len(templates))
		for i, t := range templates {
			available[i] = t.Engine
		}
		return nil, fmt.Errorf("no managed database template matches engine %q (available: %s)", engine, strings.Join(available, ", "))
	}

	labels := make([]string, len(templates))
	for i, t := range templates {
		labels[i] = fmt.Sprintf("%s %s", t.Name, t.Version)
	}
	selected, err := ui.SelectOne("Database engine:", labels)
	if err != nil {
		return nil, err
	}
	for i := range labels {
		if labels[i] == selected {
			return &templates[i], nil
		}
	}
	return nil, fmt.Errorf("invalid selection")
}

func init() {
	addCmd.Flags().StringVar(&flagAddType, "type", "", "Service type: app, bucket, database, docker, docker image, function, github, gitlab")
	addCmd.Flags().StringVar(&flagAddName, "name", "", "Service name (skips prompt)")
	addCmd.Flags().StringVar(&flagAddEngine, "engine", "", "Managed database engine (postgres, redis, mysql, mongo) — used with --type database to skip the picker")
	addCmd.Flags().StringVar(&flagAddImage, "image", "", "Docker image to deploy (e.g. nginx:latest) — sets type to docker_image")
	addCmd.Flags().StringVar(&flagAddRepo, "repo", "", "Git repo as 'owner/repo' ou URL (github.com/gitlab.com) — o tipo é detectado pelo host; --type força github|gitlab")
	addCmd.Flags().StringVar(&flagAddBranch, "branch", "main", "Git branch (used with --repo, default: main)")
	addCmd.Flags().StringVar(&flagAddRootDir, "root-dir", "", "Root directory within the repo (for monorepos, e.g. apps/api)")
	addCmd.Flags().StringVar(&flagAddBuilder, "builder", "", "Build system: dockerfile or railpack")
	addCmd.Flags().StringVar(&flagAddDockerfilePath, "dockerfile-path", "", "Path to Dockerfile (used with --builder dockerfile)")
	addCmd.Flags().StringVar(&flagAddStartCommand, "start-command", "", "Command to start the service")
	addCmd.Flags().StringVar(&flagAddHealthCheck, "health-check", "", "HTTP path for health check (e.g. /health)")
	addCmd.Flags().IntVar(&flagAddHealthCheckTimeout, "health-check-timeout", 0, "Initial delay in seconds before health checks start (default 5)")
	addCmd.Flags().StringVar(&flagAddRegistryUser, "registry-user", "", "Usuário do registry privado (com --image, imagem privada)")
	addCmd.Flags().StringVar(&flagAddRegistryPassword, "registry-password", "", "Senha/token do registry privado (armazenada como secret)")
	addCmd.Flags().StringVar(&flagAddRegistryHost, "registry-host", "", "Host do registry (ex: ghcr.io); inferido da imagem se omitido")
	rootCmd.AddCommand(addCmd)
}
