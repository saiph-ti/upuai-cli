package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var logsLines int

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View service logs",
	Long: `View logs for the linked service.

Examples:
  upuai logs
  upuai logs -n 200`,
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
	},
}

func init() {
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 100, "Number of log lines to fetch")
	rootCmd.AddCommand(logsCmd)
}
