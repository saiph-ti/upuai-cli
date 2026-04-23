package cmd

import (
	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	flagProject     string
	flagEnvironment string
	flagOutput      string
	flagYes         bool
	flagVerbose     bool
)

var rootCmd = &cobra.Command{
	Use:   "upuai",
	Short: "Upuai Cloud CLI — Smart deploy. Brazilian infrastructure.",
	Long: `Upuai Cloud CLI

Deploy, manage, and monitor your cloud infrastructure
from the command line.

Get started:
  upuai login      Authenticate with Upuai Cloud
  upuai init       Initialize a new project
  upuai deploy     Deploy your application`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		ui.PrintError(err.Error())
		return err
	}
	return nil
}

func init() {
	cobra.OnInitialize(config.InitGlobalConfig)

	rootCmd.PersistentFlags().StringVarP(&flagProject, "project", "p", "", "Project ID or name")
	rootCmd.PersistentFlags().StringVarP(&flagEnvironment, "environment", "e", "", "Environment (production, staging, development)")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "", "Output format (table, json, text)")
	rootCmd.PersistentFlags().BoolVarP(&flagYes, "yes", "y", false, "Skip confirmation prompts")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Enable verbose output")
}

func getOutputFormat() ui.OutputFormat {
	if flagOutput != "" {
		return ui.ParseOutputFormat(flagOutput)
	}
	return ui.ParseOutputFormat(config.GetDefaultOutput())
}

func getEnvironment() string {
	if flagEnvironment != "" {
		return flagEnvironment
	}
	cfg, _ := config.LoadProjectConfig()
	if cfg != nil && cfg.Environment != "" {
		return cfg.Environment
	}
	return config.GetDefaultEnvironment()
}

func getProjectID() string {
	if flagProject != "" {
		return flagProject
	}
	cfg, _ := config.LoadProjectConfig()
	if cfg != nil {
		return cfg.ProjectID
	}
	return ""
}

func requireAuth() error {
	store := config.NewCredentialStore()
	if token := store.GetToken(); token == "" {
		return errNotAuthenticated
	}
	return nil
}

func requireProject() (string, error) {
	id := getProjectID()
	if id == "" {
		return "", errNoProject
	}
	return id, nil
}

func requireServiceConfig() (string, string, error) {
	cfg, _ := config.LoadProjectConfig()
	if cfg == nil || cfg.EnvironmentID == "" || cfg.ServiceID == "" {
		return "", "", errNoServiceConfig
	}
	return cfg.EnvironmentID, cfg.ServiceID, nil
}

var (
	errNotAuthenticated = &cmdError{"not authenticated — run 'upuai login' first"}
	errNoProject        = &cmdError{"no project linked — run 'upuai init' or 'upuai link' first"}
	errNoServiceConfig  = &cmdError{"project config missing environmentId or serviceId — run 'upuai link' to reconfigure"}
)

type cmdError struct {
	msg string
}

func (e *cmdError) Error() string {
	return e.msg
}
