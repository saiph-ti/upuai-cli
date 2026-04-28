package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var runCmd = &cobra.Command{
	Use:   "run [-s SERVICE] [--] <command> [args...]",
	Short: "Run a command with service environment variables",
	Long: `Run a local command with the environment variables from your linked service injected.

The "--" separator is optional — flags after the first non-flag argument are
forwarded to the subprocess. Use "--" if your command has flags that conflict
with upuai's own (-s, -p, -e, -o, -y, -v).

Note: Secret variables are masked by the API and will not be injected.

Examples:
  upuai run npm start
  upuai run -- python manage.py migrate
  upuai run -s api -- env`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		serviceRef, cmdArgs, showHelp, err := parseRunArgs(args)
		if err != nil {
			return err
		}
		if showHelp {
			return cmd.Help()
		}
		if len(cmdArgs) == 0 {
			return fmt.Errorf("no command specified — usage: upuai run [-s SERVICE] [--] <command>")
		}

		if err := requireAuth(); err != nil {
			return err
		}

		envID, serviceID, err := resolveServiceContext(serviceRef)
		if err != nil {
			return err
		}

		env, secretCount, err := buildEnvWithVariables(envID, serviceID)
		if err != nil {
			return err
		}

		if secretCount > 0 {
			ui.PrintWarning(fmt.Sprintf("%d secret variable(s) skipped — secrets are masked by the API", secretCount))
		}
		ui.PrintInfo(fmt.Sprintf("Injected %d variable(s)", len(env)-len(os.Environ())))
		fmt.Println()

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

// parseRunArgs walks the raw args (DisableFlagParsing is on so the subprocess'
// flags don't get eaten by cobra) and pulls out upuai's own flags (-s, --help)
// before the command. Everything after a "--" is treated as command verbatim.
// Without "--", the first non-flag arg starts the command.
func parseRunArgs(args []string) (serviceRef string, cmdArgs []string, showHelp bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			cmdArgs = args[i+1:]
			return serviceRef, cmdArgs, false, nil
		case a == "-h" || a == "--help":
			return "", nil, true, nil
		case a == "-s" || a == "--service":
			if i+1 >= len(args) {
				return "", nil, false, fmt.Errorf("flag %s requires a value", a)
			}
			serviceRef = args[i+1]
			i++
		case strings.HasPrefix(a, "--service="):
			serviceRef = strings.TrimPrefix(a, "--service=")
		case strings.HasPrefix(a, "-s="):
			serviceRef = strings.TrimPrefix(a, "-s=")
		default:
			cmdArgs = args[i:]
			return serviceRef, cmdArgs, false, nil
		}
	}
	return serviceRef, cmdArgs, false, nil
}

// buildEnvWithVariables fetches service env vars from the API and merges them
// onto os.Environ(). Secrets are skipped (the API masks them anyway). Returns
// the new environ slice and the count of secrets that were skipped.
func buildEnvWithVariables(envID, serviceID string) ([]string, int, error) {
	client := api.NewClient()

	var vars []api.EnvVar
	err := ui.RunWithSpinner("Loading variables...", func() error {
		var fetchErr error
		vars, fetchErr = client.ListVariables(envID, serviceID)
		return fetchErr
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch variables: %w", err)
	}

	env := os.Environ()
	secretCount := 0
	for _, v := range vars {
		if v.IsSecret {
			secretCount++
			continue
		}
		value := v.Value
		if v.ResolvedValue != "" {
			value = v.ResolvedValue
		}
		env = append(env, fmt.Sprintf("%s=%s", v.Key, value))
	}
	return env, secretCount, nil
}

func init() {
	rootCmd.AddCommand(runCmd)
}
