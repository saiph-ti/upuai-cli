package cmd

import (
	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out from Upuai Cloud",
	RunE: func(cmd *cobra.Command, args []string) error {
		store := config.NewCredentialStore()

		if !store.Exists() {
			ui.PrintInfo("Already logged out")
			return nil
		}

		if err := store.Clear(); err != nil {
			return err
		}

		ui.PrintSuccess("Logged out successfully")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
