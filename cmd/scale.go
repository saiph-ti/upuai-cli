package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var scaleCmd = &cobra.Command{
	Use:   "scale <count> | <name>=<count> [<name>=<count>...]",
	Short: "Scale the service or a specific process to N replicas",
	Long: `Scale the linked service to a number of replicas.

Pass a bare integer to scale the whole (single-process) service, or one or
more <process>=<count> pairs to scale individual processes of a multi-process
service (web + worker + clock — Procfile parity). Use "upuai ps" to list the
service's processes.

Examples:
  upuai scale 3                # scale the service to 3 replicas
  upuai scale web=2 worker=1   # scale process "web" to 2 and "worker" to 1`,
	Args: cobra.MinimumNArgs(1),
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

		client := api.NewClient()

		// Legacy form: a single bare integer scales the whole service.
		if len(args) == 1 {
			if count, convErr := strconv.Atoi(args[0]); convErr == nil {
				if count < 0 {
					return fmt.Errorf("invalid replica count %q — must be a non-negative integer", args[0])
				}
				return scaleWhole(client, envID, serviceID, count)
			}
		}

		// Per-process form: every arg must be <name>=<count>.
		type scaleTarget struct {
			name  string
			count int
		}
		targets := make([]scaleTarget, 0, len(args))
		for _, arg := range args {
			name, raw, ok := strings.Cut(arg, "=")
			if !ok || name == "" {
				return fmt.Errorf("invalid argument %q — expected a bare integer (e.g. 3) or <process>=<count> (e.g. web=2)", arg)
			}
			count, convErr := strconv.Atoi(raw)
			if convErr != nil || count < 0 {
				return fmt.Errorf("invalid replica count %q for process %q — must be a non-negative integer", raw, name)
			}
			targets = append(targets, scaleTarget{name: name, count: count})
		}

		for _, t := range targets {
			t := t
			err = ui.RunWithSpinner(fmt.Sprintf("Scaling %s to %d replica(s)...", t.name, t.count), func() error {
				return client.ScaleInstance(envID, serviceID, t.name, t.count)
			})
			if err != nil {
				return fmt.Errorf("scale %s failed: %w", t.name, err)
			}
			ui.PrintSuccess(fmt.Sprintf("Scaled %s to %d replica(s)", t.name, t.count))
		}
		return nil
	},
}

func scaleWhole(client *api.Client, envID, serviceID string, count int) error {
	err := ui.RunWithSpinner(fmt.Sprintf("Scaling to %d replica(s)...", count), func() error {
		return client.ScaleInstance(envID, serviceID, "", count)
	})
	if err != nil {
		return fmt.Errorf("scale failed: %w", err)
	}
	ui.PrintSuccess(fmt.Sprintf("Scaled to %d replica(s)", count))
	return nil
}

func init() {
	rootCmd.AddCommand(scaleCmd)
}
