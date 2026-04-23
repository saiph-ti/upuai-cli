package cmd

import (
	"fmt"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var openCmd = &cobra.Command{
	Use:   "open",
	Short: "Open the project in the browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, err := requireProject()
		if err != nil {
			return err
		}

		url := fmt.Sprintf("%s/projects/%s", config.GetWebURL(), projectID)

		if err := browser.OpenURL(url); err != nil {
			return fmt.Errorf("failed to open browser: %w", err)
		}

		ui.PrintSuccess(fmt.Sprintf("Opened %s", url))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(openCmd)
}
