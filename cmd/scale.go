package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var scaleCmd = &cobra.Command{
	Use:   "scale <count>",
	Short: "Scale service to N replicas",
	Args:  cobra.ExactArgs(1),
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

		count, err := strconv.Atoi(args[0])
		if err != nil || count < 0 {
			return fmt.Errorf("invalid replica count %q — must be a non-negative integer", args[0])
		}

		client := api.NewClient()

		err = ui.RunWithSpinner(fmt.Sprintf("Scaling to %d replica(s)...", count), func() error {
			return client.ScaleInstance(envID, serviceID, count)
		})
		if err != nil {
			return fmt.Errorf("scale failed: %w", err)
		}

		ui.PrintSuccess(fmt.Sprintf("Scaled to %d replica(s)", count))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(scaleCmd)
}
