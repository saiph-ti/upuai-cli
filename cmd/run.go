package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var runCmd = &cobra.Command{
	Use:   "run -- <command>",
	Short: "Run a command with service environment variables",
	Long: `Run a local command with the environment variables from your linked service injected.

Note: Secret variables are masked by the API and will not be injected.

Examples:
  upuai run -- npm start
  upuai run -- python manage.py migrate
  upuai run -- env`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Find "--" separator
		cmdArgs := args
		for i, arg := range args {
			if arg == "--" {
				cmdArgs = args[i+1:]
				break
			}
		}

		if len(cmdArgs) == 0 {
			return fmt.Errorf("no command specified — usage: upuai run -- <command>")
		}

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

		var vars []api.EnvVar
		err = ui.RunWithSpinner("Loading variables...", func() error {
			var fetchErr error
			vars, fetchErr = client.ListVariables(envID, serviceID)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to fetch variables: %w", err)
		}

		// Build environment
		env := os.Environ()
		secretCount := 0
		for _, v := range vars {
			if v.IsSecret {
				secretCount++
				continue
			}
			env = append(env, fmt.Sprintf("%s=%s", v.Key, v.Value))
		}

		if secretCount > 0 {
			ui.PrintWarning(fmt.Sprintf("%d secret variable(s) skipped — secrets are masked by the API", secretCount))
		}

		ui.PrintInfo(fmt.Sprintf("Injected %d variable(s)", len(vars)-secretCount))
		fmt.Println()

		// Execute command
		child := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		child.Env = env
		child.Stdin = os.Stdin
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr

		if err := child.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			return fmt.Errorf("failed to run command: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
