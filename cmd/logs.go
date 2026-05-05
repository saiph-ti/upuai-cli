package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	logsLines        int
	logsBuild        bool
	logsDeploy       bool
	logsFollow       bool
	logsTimeline     bool
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

		if logsTimeline {
			return runTimeline(client)
		}
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

// runTimeline renders the structured DeploymentTimeline for the latest (or
// --deployment) deploy. Stack-agnostic — the platform projection works
// identically for any framework. Combined with -f, polls every 2s while the
// deployment is in-progress; bails out once status reaches a terminal value.
//
// See runbook 2026-05-05-deployment-timeline.md for the canonical schema.
func runTimeline(client *api.Client) error {
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
	}

	terminal := map[string]bool{
		"DEPLOYMENT_STATUS_LIVE":      true,
		"DEPLOYMENT_STATUS_FAILED":    true,
		"DEPLOYMENT_STATUS_CANCELLED": true,
	}

	for {
		tl, err := client.GetDeploymentTimeline(deployID)
		if err != nil {
			return fmt.Errorf("failed to fetch timeline: %w", err)
		}
		if tl == nil {
			ui.PrintInfo("Timeline not available yet")
			if !logsFollow {
				return nil
			}
			time.Sleep(2 * time.Second)
			continue
		}

		printTimeline(tl)

		if !logsFollow || terminal[tl.Status] {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
}

// printTimeline renders the structured timeline to stdout. Plain text with
// minimal coloring for terminal-friendliness; never tries to interpret log
// content (last_lines printed verbatim).
func printTimeline(tl *api.DeploymentTimeline) {
	fmt.Println()
	ui.PrintKeyValue("Deployment", tl.DeploymentID)
	ui.PrintKeyValue("Status", strings.TrimPrefix(tl.Status, "DEPLOYMENT_STATUS_"))
	if tl.Partial {
		ui.PrintWarning("Partial timeline — some sources unavailable")
	}

	if tl.FailureSummary != nil && len(tl.FailureSummary.LastLines) > 0 {
		fmt.Println()
		header := fmt.Sprintf("✗ %s — %s",
			strings.TrimPrefix(tl.FailureSummary.Stage, "STAGE_KIND_"),
			tl.FailureSummary.Step)
		if tl.FailureSummary.ExitCode != nil {
			header += fmt.Sprintf(" (exit %d)", *tl.FailureSummary.ExitCode)
		}
		fmt.Println(ui.Error.Render(header))
		for _, line := range tl.FailureSummary.LastLines {
			fmt.Println("  " + line)
		}
	}

	for _, st := range tl.Stages {
		fmt.Println()
		kind := strings.TrimPrefix(st.Kind, "STAGE_KIND_")
		status := strings.TrimPrefix(st.Status, "STAGE_STATUS_")
		dur := ""
		if st.DurationMs > 0 {
			dur = fmt.Sprintf(" %.1fs", float64(st.DurationMs)/1000.0)
		}
		fmt.Printf("[%s] %s%s\n", status, kind, dur)

		if st.Build != nil {
			if st.Build.Builder != "" {
				fmt.Printf("  builder: %s\n", st.Build.Builder)
			}
			if st.Build.Detected != nil {
				if st.Build.Detected.Language != "" || st.Build.Detected.Framework != "" {
					fmt.Printf("  detected: %s / %s\n",
						st.Build.Detected.Language, st.Build.Detected.Framework)
				}
			}
			for _, bs := range st.Build.BuildkitSteps {
				bstatus := strings.TrimPrefix(bs.Status, "BUILDKIT_STEP_STATUS_")
				bdur := ""
				if bs.DurationMs > 0 {
					bdur = fmt.Sprintf(" %.1fs", float64(bs.DurationMs)/1000.0)
				}
				name := bs.Name
				if len(name) > 80 {
					name = name[:77] + "..."
				}
				fmt.Printf("    %s [%s] %s%s\n", bs.ID, bstatus, name, bdur)
			}
		}
		if st.Deploy != nil && len(st.Deploy.Pods) > 0 {
			fmt.Printf("  rollout: %s\n", strings.TrimPrefix(st.Deploy.RolloutPhase, "ROLLOUT_PHASE_"))
			for _, p := range st.Deploy.Pods {
				fmt.Printf("    pod %s (%s)\n", p.Name, p.Phase)
				for _, c := range p.Containers {
					line := fmt.Sprintf("      %s ready=%t restarts=%d", c.Name, c.Ready, c.RestartCount)
					if c.LastTermination != nil {
						line += fmt.Sprintf(" · %s exit=%d",
							c.LastTermination.Reason, c.LastTermination.ExitCode)
					}
					fmt.Println(line)
				}
			}
		}
		if st.GitClone != nil && st.Status == "STAGE_STATUS_FAILED" {
			if st.GitClone.TerminationMessage != "" {
				fmt.Printf("  message: %s\n", st.GitClone.TerminationMessage)
			}
		}
	}
}

func init() {
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 100, "Number of log lines to fetch (runtime only)")
	logsCmd.Flags().BoolVar(&logsBuild, "build", false, "Show the build log of a deployment")
	logsCmd.Flags().BoolVar(&logsDeploy, "deploy", false, "Show the release-phase + rollout log of a deployment")
	logsCmd.Flags().BoolVar(&logsTimeline, "timeline", false, "Show the structured deployment timeline (stack-agnostic stage/step view + failure summary)")
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Stream runtime logs (live tail via SSE), or poll timeline every 2s with --timeline")
	logsCmd.Flags().StringVarP(&logsDeploymentID, "deployment", "d", "", "Specific deployment ID (default: latest)")
	logsCmd.Flags().StringVarP(&logsServiceRef, "service", "s", "", "Service ref (name|slug|id) — overrides linked service")
	rootCmd.AddCommand(logsCmd)
}
