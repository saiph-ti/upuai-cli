package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
	"github.com/upuai-cloud/cli/internal/watcher"
)

var (
	deployWatchFlag       bool
	deployWaitFlag        bool
	deployWaitTimeoutFlag int
)

// terminalDeployStatuses mirrors the DeploymentStatus enum in
// apps/shared/src/types/deployment-types.ts. Polling stops once status hits
// any of these.
var terminalDeployStatuses = map[string]struct{}{
	"success":      {},
	"failed":       {},
	"cancelled":    {},
	"build_failed": {},
	"superseded":   {},
}

// failedDeployStatuses are terminal statuses that should yield a non-zero
// exit code from `upuai deploy --wait`. `superseded` is excluded — it just
// means a newer deploy raced ahead and is not itself a user-visible failure.
var failedDeployStatuses = map[string]struct{}{
	"failed":       {},
	"cancelled":    {},
	"build_failed": {},
}

var deployCmd = &cobra.Command{
	Use:     "deploy",
	Aliases: []string{"up"},
	Short:   "Deploy the current project",
	Long: `Deploy the current project to Upuai Cloud.

Triggers a new deployment for the linked project. By default the command
returns immediately after the API accepts the request; the build, release
phase, and rollout continue asynchronously. Pass --wait to poll until the
deployment reaches a terminal status (success, failed, cancelled,
build_failed, or superseded). Exit code is non-zero on failed, cancelled,
or build_failed.

Use --watch for auto-redeploy on local file changes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		env := getEnvironment()

		cfg, _ := config.LoadProjectConfig()
		serviceID := ""
		if cfg != nil {
			serviceID = cfg.ServiceID
		}

		if err := runDeploy(projectID, env, serviceID); err != nil {
			return err
		}

		if deployWatchFlag {
			return runWatchMode(projectID, env, serviceID)
		}

		return nil
	},
}

func runDeploy(projectID, env, serviceID string) error {
	client := api.NewClient()
	var deployment *api.Deployment

	err := ui.RunWithSpinner("Deploying to "+env+"...", func() error {
		var deployErr error
		deployment, deployErr = client.Deploy(projectID, &api.DeployRequest{
			Environment: env,
			ServiceID:   serviceID,
		})
		return deployErr
	})
	if err != nil {
		return fmt.Errorf("deployment failed: %w", err)
	}

	format := getOutputFormat()

	// When --wait is set, replace the immediate-return semantics with a poll
	// loop. The terminal Deployment becomes the value rendered/JSON-printed.
	if deployWaitFlag {
		final, waitErr := waitForDeployment(client, deployment.ID, format)
		if waitErr != nil {
			return waitErr
		}
		deployment = final
		if _, isFail := failedDeployStatuses[strings.ToLower(deployment.Status)]; isFail {
			// Non-zero exit so CI / agents can branch on outcome.
			return fmt.Errorf("deployment %s ended with status %q", deployment.ID, deployment.Status)
		}
	}

	if format == ui.FormatJSON {
		ui.PrintJSON(deployment)
		return nil
	}

	fmt.Println()
	if deployWaitFlag {
		ui.PrintSuccess(fmt.Sprintf("Deployment %s", strings.ToLower(deployment.Status)))
	} else {
		ui.PrintSuccess("Deployment triggered!")
	}
	fmt.Println()
	kv := []string{
		"Deployment", deployment.ID,
		"Status", deployment.Status,
	}
	// Builder default is railpack for every deploy regardless of repo content.
	// dockerfile is opt-in only — set explicitly via `upuai config set
	// --builder dockerfile`. Having a Dockerfile in the repo does NOT change
	// the build. We surface the resolved value once the orchestrator persists
	// it on the deployment row.
	if deployment.Builder != "" {
		kv = append(kv, "Builder", deployment.Builder)
		if deployment.DockerfilePath != "" {
			kv = append(kv, "Dockerfile", deployment.DockerfilePath)
		}
	} else {
		kv = append(kv, "Builder", "railpack (default)")
	}
	if deployment.URL != "" {
		kv = append(kv, "URL", deployment.URL)
	}
	ui.PrintKeyValue(kv...)
	fmt.Println()
	if !deployWaitFlag {
		ui.PrintInfo("Pass --wait to block until the deployment reaches a terminal status.")
	}
	ui.PrintInfo("Switch to dockerfile (opt-in): `upuai config set --builder dockerfile --dockerfile-path Dockerfile`")
	fmt.Println()

	return nil
}

// waitForDeployment polls GetDeployment every 3 seconds until the deployment
// hits a terminal status or the timeout expires. Prints status transitions to
// stderr in text mode; stays silent in JSON mode (caller does the rendering).
func waitForDeployment(client *api.Client, deployID string, format ui.OutputFormat) (*api.Deployment, error) {
	timeout := time.Duration(deployWaitTimeoutFlag) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	const pollInterval = 3 * time.Second

	deadline := time.Now().Add(timeout)
	lastStatus := ""

	for {
		dep, err := client.GetDeployment(deployID)
		if err != nil {
			return nil, fmt.Errorf("polling deployment %s: %w", deployID, err)
		}

		status := strings.ToLower(dep.Status)
		if status != lastStatus {
			if format != ui.FormatJSON {
				ui.PrintInfo(fmt.Sprintf("→ %s", dep.Status))
			}
			lastStatus = status
		}

		if _, terminal := terminalDeployStatuses[status]; terminal {
			return dep, nil
		}

		if time.Now().After(deadline) {
			return dep, fmt.Errorf("timed out after %s waiting for deployment %s (last status: %s)", timeout, deployID, dep.Status)
		}

		time.Sleep(pollInterval)
	}
}

func runWatchMode(projectID, env, serviceID string) error {
	fmt.Println()
	ui.PrintInfo("Watching for file changes... (press Ctrl+C to stop)")
	fmt.Println()

	deployCount := 0
	return watcher.Watch(".", 500*time.Millisecond, func() error {
		deployCount++
		fmt.Printf("\n%s File change detected (deploy #%d)\n",
			ui.Info.Render("→"), deployCount)
		return runDeploy(projectID, env, serviceID)
	})
}

func init() {
	deployCmd.Flags().BoolVarP(&deployWatchFlag, "watch", "w", false, "Watch for changes and auto-redeploy")
	deployCmd.Flags().BoolVar(&deployWaitFlag, "wait", false, "Block until the deployment reaches a terminal status (success, failed, cancelled, build_failed, superseded). Exits non-zero on failure.")
	deployCmd.Flags().IntVar(&deployWaitTimeoutFlag, "wait-timeout", 300, "Maximum seconds to wait when --wait is set (default 300)")
	rootCmd.AddCommand(deployCmd)
}
