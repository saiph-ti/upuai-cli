package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
	"github.com/upuai-cloud/cli/internal/updatecheck"
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
	// Resolve the active subcommand name *before* running, so an error doesn't
	// prevent us from filtering the update nudge correctly (a 'help' shown
	// after an unknown flag still returns an error from Execute()).
	executed, _, _ := rootCmd.Find(os.Args[1:])
	cmdName := ""
	if executed != nil {
		cmdName = executed.Name()
	}

	err := rootCmd.Execute()
	if err != nil {
		ui.PrintError(err.Error())
	}

	// Update notification runs *after* the user's command — never block their
	// workflow. Errors inside MaybeNotify are silently swallowed.
	updatecheck.MaybeNotify(cmdName)

	return err
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

// resolveServiceContext returns (envID, serviceID) for the current command.
// If serviceRef is empty, it falls back to the linked service in .upuai/config.json
// (same as requireServiceConfig). If serviceRef is provided, it resolves the
// environment from the -e flag (or the linked env, or the default), then matches
// the service by name, slug, or ID against the project's service list.
func resolveServiceContext(serviceRef string) (envID, serviceID string, err error) {
	if serviceRef == "" {
		return requireServiceConfig()
	}

	projectID, err := requireProject()
	if err != nil {
		return "", "", err
	}

	client := api.NewClient()

	envID, err = resolveEnvironmentID(client, projectID)
	if err != nil {
		return "", "", err
	}

	services, err := client.ListServices(projectID)
	if err != nil {
		return "", "", fmt.Errorf("list services: %w", err)
	}
	for _, s := range services {
		if s.ID == serviceRef ||
			strings.EqualFold(s.Name, serviceRef) ||
			strings.EqualFold(s.Slug, serviceRef) {
			return envID, s.ID, nil
		}
	}
	return "", "", fmt.Errorf("service %q not found in project — try 'upuai list' to see available services", serviceRef)
}

// resolveEnvironmentID picks the environment ID using the same priority as the
// rest of the CLI: explicit -e flag → linked .upuai/config.json → default name
// from global config (resolved against the project's environment list).
func resolveEnvironmentID(client *api.Client, projectID string) (string, error) {
	cfg, _ := config.LoadProjectConfig()

	// Linked envID wins when no -e was passed.
	if flagEnvironment == "" && cfg != nil && cfg.EnvironmentID != "" {
		return cfg.EnvironmentID, nil
	}

	envName := flagEnvironment
	if envName == "" {
		if cfg != nil && cfg.Environment != "" {
			envName = cfg.Environment
		} else {
			envName = config.GetDefaultEnvironment()
		}
	}

	envs, err := client.ListEnvironments(projectID)
	if err != nil {
		return "", fmt.Errorf("list environments: %w", err)
	}
	for _, e := range envs {
		if e.ID == envName || strings.EqualFold(e.Name, envName) {
			return e.ID, nil
		}
	}
	return "", fmt.Errorf("environment %q not found in project", envName)
}

// consumeLeadingFlag handles upuai's OWN flags that appear before the passthrough
// command in DisableFlagParsing commands (`ssh`, `run`). Because those commands turn
// off cobra flag parsing (so they don't eat flags meant for the remote/subprocess
// program, e.g. `rails console -e production`), cobra never populates the persistent
// flag globals. Without this, `upuai ssh -p adv-os -s web -- cmd` silently dropped
// -p/-e and fell back to the linked service. This recognizes the value-taking flags
// (-p/--project, -e/--environment, -o/--output, -s/--service, incl. --name=value /
// -x=value forms) and the boolean flags (-y/--yes, -v/--verbose), setting the same
// globals cobra would, so the rest of the CLI (getProjectID/resolveEnvironmentID/…)
// behaves identically to a normally-parsed command. serviceRef is threaded out via a
// pointer because it is command-local (not a persistent flag).
//
// Returns: consumed = number of args used (1 for =form/boolean, 2 for "-x value");
// matched = whether `a` was a known upuai flag; err = a value-required error.
func consumeLeadingFlag(args []string, i int, serviceRef *string) (consumed int, matched bool, err error) {
	a := args[i]
	valueFlags := []struct {
		names []string
		set   func(string)
	}{
		{[]string{"-p", "--project"}, func(v string) { flagProject = v }},
		{[]string{"-e", "--environment"}, func(v string) { flagEnvironment = v }},
		{[]string{"-o", "--output"}, func(v string) { flagOutput = v }},
		{[]string{"-s", "--service"}, func(v string) { *serviceRef = v }},
	}
	for _, vf := range valueFlags {
		for _, n := range vf.names {
			if a == n {
				if i+1 >= len(args) {
					return 0, true, fmt.Errorf("flag %s requires a value", n)
				}
				vf.set(args[i+1])
				return 2, true, nil
			}
			if strings.HasPrefix(a, n+"=") {
				vf.set(strings.TrimPrefix(a, n+"="))
				return 1, true, nil
			}
		}
	}
	boolFlags := []struct {
		names []string
		set   func()
	}{
		{[]string{"-y", "--yes"}, func() { flagYes = true }},
		{[]string{"-v", "--verbose"}, func() { flagVerbose = true }},
	}
	for _, bf := range boolFlags {
		for _, n := range bf.names {
			if a == n {
				bf.set()
				return 1, true, nil
			}
		}
	}
	return 0, false, nil
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
