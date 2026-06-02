package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var restartProcess string

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the linked service",
	Long: `Restart the running container(s) of the linked service.

By default (no --process) the whole service / web process is restarted. Pass
--process to restart a single process of a multi-process service (see
"upuai ps").

Examples:
  upuai restart                  # restart the service (web)
  upuai restart --process worker # restart only the "worker" process`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		if _, err := requireProject(); err != nil {
			return err
		}

		envID, serviceID, err := requireServiceConfig()
		if err != nil {
			return err
		}

		client := api.NewClient()

		// Resolve the process name→canonical up front when --process is set, so the
		// user gets a friendly "process not found" instead of a generic API error
		// (mirrors `scheduler run`, which resolves the job before acting).
		processName := ""
		target := "the service"
		if restartProcess != "" {
			proc, perr := resolveProcess(client, envID, serviceID, restartProcess)
			if perr != nil {
				return perr
			}
			processName = proc.Name
			target = fmt.Sprintf("process %q", proc.Name)
		}

		if !flagYes {
			confirmed, err := ui.Confirm(fmt.Sprintf("Restart %s?", target))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Restart cancelled")
				return nil
			}
		}

		err = ui.RunWithSpinner(fmt.Sprintf("Restarting %s...", target), func() error {
			return client.RestartInstance(envID, serviceID, processName)
		})
		if err != nil {
			return fmt.Errorf("restart failed: %w", err)
		}

		ui.PrintSuccess(fmt.Sprintf("Restarted %s successfully", target))
		return nil
	},
}

func init() {
	restartCmd.Flags().StringVar(&restartProcess, "process", "", "Process name to restart (multi-process service; default: web; see 'upuai ps')")
	rootCmd.AddCommand(restartCmd)
}
