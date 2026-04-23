package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/ui"
)

var unlinkCmd = &cobra.Command{
	Use:   "unlink",
	Short: "Unlink current directory from the project",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := filepath.Join(".upuai", "config.json")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			ui.PrintInfo("No project linked in this directory")
			return nil
		}

		if !flagYes {
			confirmed, err := ui.Confirm("Unlink this directory from the project?")
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Unlink cancelled")
				return nil
			}
		}

		if err := os.Remove(configPath); err != nil {
			return fmt.Errorf("failed to remove config: %w", err)
		}

		ui.PrintSuccess("Project unlinked")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(unlinkCmd)
}
