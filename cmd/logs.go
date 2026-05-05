package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	logsLines        int
	logsBuild        bool
	logsDeploy       bool
	logsFollow       bool
	logsDeploymentID string
	logsServiceRef   string
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View service logs (runtime, build, or release-phase)",
	Long: `View logs for the linked service.

By default, fetches the most recent runtime logs from the running container.
Use --build or --deploy to inspect a specific deployment instead.

Examples:
  upuai logs                          # last 100 runtime log lines
  upuai logs -n 200                   # last 200 runtime log lines
  upuai logs -f                       # stream runtime logs (live tail)
  upuai logs --build                  # build log of the latest deployment
  upuai logs --deploy                 # release-phase + rollout log of the latest deployment
  upuai logs --build -d <deploy-id>   # build log of a specific deployment`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		if _, err := requireProject(); err != nil {
			return err
		}

		if logsBuild && logsDeploy {
			return fmt.Errorf("--build and --deploy are mutually exclusive")
		}

		client := api.NewClient()

		if logsBuild || logsDeploy {
			return runDeploymentLogs(client)
		}
		return runRuntimeLogs(client)
	},
}

func runRuntimeLogs(client *api.Client) error {
	envID, serviceID, err := resolveServiceContext(logsServiceRef)
	if err != nil {
		return err
	}

	if logsFollow {
		ctx, cancel := signalCtx()
		defer cancel()
		ui.PrintInfo("Streaming runtime logs (Ctrl-C to stop)")
		fmt.Println()
		return client.StreamRuntimeLogs(ctx, envID, serviceID, func(line string) {
			fmt.Println(line)
		})
	}

	var logs string
	err = ui.RunWithSpinner("Fetching logs...", func() error {
		var fetchErr error
		logs, fetchErr = client.GetLogs(envID, serviceID, logsLines)
		return fetchErr
	})
	if err != nil {
		return fmt.Errorf("failed to fetch logs: %w", err)
	}

	if logs == "" {
		ui.PrintInfo("No logs available")
		return nil
	}
	fmt.Print(logs)
	return nil
}

func runDeploymentLogs(client *api.Client) error {
	deployID := logsDeploymentID
	if deployID == "" {
		envID, serviceID, err := resolveServiceContext(logsServiceRef)
		if err != nil {
			return err
		}
		var latest *api.Deployment
		err = ui.RunWithSpinner("Resolving latest deployment...", func() error {
			var lerr error
			latest, lerr = client.LatestDeployment(envID, serviceID)
			return lerr
		})
		if err != nil {
			return fmt.Errorf("failed to find latest deployment: %w", err)
		}
		if latest == nil {
			ui.PrintInfo("No deployments yet for this service")
			return nil
		}
		deployID = latest.ID
		ui.PrintInfo("Deployment: " + ui.Accent.Render(deployID) + " (" + latest.Status + ")")
		fmt.Println()
	}

	ctx, cancel := signalCtx()
	defer cancel()

	stream := client.StreamDeployLogs
	label := "deploy"
	if logsBuild {
		stream = client.StreamBuildLogs
		label = "build"
	}

	err := stream(ctx, deployID, func(line string) {
		fmt.Println(line)
	})
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("failed to stream %s logs: %w", label, err)
	}
	return nil
}

func signalCtx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func init() {
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 100, "Number of log lines to fetch (runtime only)")
	logsCmd.Flags().BoolVar(&logsBuild, "build", false, "Show the build log of a deployment")
	logsCmd.Flags().BoolVar(&logsDeploy, "deploy", false, "Show the release-phase + rollout log of a deployment")
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Stream runtime logs (live tail via SSE)")
	logsCmd.Flags().StringVarP(&logsDeploymentID, "deployment", "d", "", "Specific deployment ID (default: latest)")
	logsCmd.Flags().StringVarP(&logsServiceRef, "service", "s", "", "Service ref (name|slug|id) — overrides linked service")
	rootCmd.AddCommand(logsCmd)
}
