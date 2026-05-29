package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/archive"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	upWaitFlag        bool
	upWaitTimeoutFlag int
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Deploy the current directory (uploads local source, no git connection needed)",
	Long: `Deploy the current working directory to Upuai Cloud.

Empacota o diretório local num tarball (honrando .gitignore/.upuaiignore,
excluindo .git/node_modules/.env*), faz upload pro storage da plataforma e
dispara um deploy a partir dessa fonte — sem precisar de repositório git
conectado. Mesmo padrão de 'vercel' / 'railway up' / 'fly deploy'.

Para deploy a partir de um repositório git conectado, use 'upuai deploy'.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		projectID, err := requireProject()
		if err != nil {
			return err
		}
		envID, serviceID, err := requireServiceConfig()
		if err != nil {
			return err
		}
		env := getEnvironment()

		// upuai.toml local (se houver) é enviado pra API resolver release-phase/
		// build/start — paridade com o caminho git (que lê via API do provider).
		upuaiToml := ""
		if data, readErr := os.ReadFile("upuai.toml"); readErr == nil {
			upuaiToml = string(data)
		}

		// 1. Empacota a fonte local.
		var tarPath, sum string
		var size int64
		err = ui.RunWithSpinner("Packing source...", func() error {
			var packErr error
			tarPath, sum, size, packErr = archive.Pack(".")
			return packErr
		})
		if err != nil {
			return fmt.Errorf("pack source: %w", err)
		}
		defer func() { _ = os.Remove(tarPath) }()

		client := api.NewClient()

		// 2. Pede a presigned PUT URL (gate de plano server-side pelo tamanho).
		var upload *api.SourceUpload
		err = ui.RunWithSpinner("Preparing upload...", func() error {
			var upErr error
			upload, upErr = client.CreateSourceUpload(envID, serviceID, size)
			return upErr
		})
		if err != nil {
			return fmt.Errorf("prepare upload: %w", err)
		}

		// 3. Upload do tarball direto pro storage.
		err = ui.RunWithSpinner(fmt.Sprintf("Uploading source (%s)...", humanSize(size)), func() error {
			return client.UploadSource(upload.UploadURL, tarPath, size)
		})
		if err != nil {
			return err
		}

		// 4. Dispara o deploy referenciando o tarball.
		var deployment *api.Deployment
		err = ui.RunWithSpinner("Deploying to "+env+"...", func() error {
			var depErr error
			deployment, depErr = client.Deploy(projectID, &api.DeployRequest{
				Environment:   env,
				ServiceID:     serviceID,
				ArchiveKey:    upload.ObjectKey,
				ArchiveSha256: sum,
				UpuaiToml:     upuaiToml,
			})
			return depErr
		})
		if err != nil {
			return fmt.Errorf("deployment failed: %w", err)
		}

		format := getOutputFormat()

		if upWaitFlag {
			// waitForDeployment (deploy.go) lê o timeout do flag compartilhado.
			deployWaitTimeoutFlag = upWaitTimeoutFlag
			final, waitErr := waitForDeployment(client, deployment.ID, format)
			if waitErr != nil {
				return waitErr
			}
			deployment = final
			if _, isFail := failedDeployStatuses[strings.ToLower(deployment.Status)]; isFail {
				if deployment.ErrorMessage != "" && format != ui.FormatJSON {
					fmt.Println()
					ui.PrintError(deployment.ErrorMessage)
				}
				return fmt.Errorf("deployment %s ended with status %q", deployment.ID, deployment.Status)
			}
		}

		if format == ui.FormatJSON {
			ui.PrintJSON(deployment)
			return nil
		}

		fmt.Println()
		if upWaitFlag {
			ui.PrintSuccess(fmt.Sprintf("Deployment %s", strings.ToLower(deployment.Status)))
		} else {
			ui.PrintSuccess("Source uploaded and deployment triggered!")
		}
		fmt.Println()
		kv := []string{"Deployment", deployment.ID, "Status", deployment.Status}
		if deployment.URL != "" {
			kv = append(kv, "URL", deployment.URL)
		}
		ui.PrintKeyValue(kv...)
		fmt.Println()
		if !upWaitFlag {
			ui.PrintInfo("Pass --wait to block until the deployment reaches a terminal status.")
		}
		fmt.Println()
		return nil
	},
}

// humanSize formata bytes em KB/MB pra feedback do upload.
func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func init() {
	upCmd.Flags().BoolVar(&upWaitFlag, "wait", false, "Block until the deployment reaches a terminal status. Exits non-zero on failure.")
	upCmd.Flags().IntVar(&upWaitTimeoutFlag, "wait-timeout", 300, "Maximum seconds to wait when --wait is set (default 300)")
	rootCmd.AddCommand(upCmd)
}
