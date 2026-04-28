package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	shellService   string
	shellSilent    bool
	shellShellPath string
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open a subshell with the linked service's environment variables injected",
	Long: `Open an interactive subshell with all environment variables of the linked
service injected — equivalent to "railway shell".

The shell defaults to $SHELL on Unix and %COMSPEC% (cmd.exe) on Windows. Override
with --shell or by setting $SHELL.

Secret variables are masked by the API and will not be injected.

Examples:
  upuai shell
  upuai shell -s api
  upuai shell --shell /bin/zsh`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		envID, serviceID, err := resolveServiceContext(shellService)
		if err != nil {
			return err
		}

		env, secretCount, err := buildEnvWithVariables(envID, serviceID)
		if err != nil {
			return err
		}

		shellPath, err := pickShell(shellShellPath)
		if err != nil {
			return err
		}

		if !shellSilent {
			injected := len(env) - len(os.Environ())
			ui.PrintInfo(fmt.Sprintf("Spawning %s with %d variable(s) from service", shellPath, injected))
			if secretCount > 0 {
				ui.PrintWarning(fmt.Sprintf("%d secret(s) skipped — secrets are masked by the API", secretCount))
			}
			ui.PrintInfo("Type 'exit' to leave the subshell")
			fmt.Println()
		}

		child := exec.Command(shellPath)
		child.Env = env
		child.Stdin = os.Stdin
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr

		if err := child.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			return fmt.Errorf("subshell failed: %w", err)
		}
		return nil
	},
}

func pickShell(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("COMSPEC"); v != "" {
			return v, nil
		}
		return "cmd.exe", nil
	}
	if v := os.Getenv("SHELL"); v != "" {
		return v, nil
	}
	return "/bin/sh", nil
}

func init() {
	shellCmd.Flags().StringVarP(&shellService, "service", "s", "", "Service name, slug, or ID (overrides linked service)")
	shellCmd.Flags().BoolVar(&shellSilent, "silent", false, "Suppress the spawn banner")
	shellCmd.Flags().StringVar(&shellShellPath, "shell", "", "Path to the shell binary (default: $SHELL or cmd.exe)")
	rootCmd.AddCommand(shellCmd)
}
