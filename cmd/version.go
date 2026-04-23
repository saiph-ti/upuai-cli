package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/ui"
	"github.com/upuai-cloud/cli/pkg/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		format := getOutputFormat()
		switch format {
		case ui.FormatJSON:
			ui.PrintJSON(map[string]string{
				"version":   version.Version,
				"commit":    version.Commit,
				"buildDate": version.BuildDate,
			})
		default:
			fmt.Println(version.Full())
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)

	rootCmd.Version = version.Short()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}
