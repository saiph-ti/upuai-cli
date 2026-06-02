package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var processService string

var processCmd = &cobra.Command{
	Use:     "ps",
	Aliases: []string{"processes", "process"},
	Short:   "List the service's processes (web, worker, clock, release)",
	Long: `List the declared processes of the linked service.

Multi-process services run several process types (web + worker + clock +
release) from a single repo and build — Procfile / Heroku / Railway parity.
Scale a single process with "upuai scale <name>=<N>".

Examples:
  upuai ps                # processes of the linked service
  upuai ps -s api         # processes of service "api"
  upuai ps -o json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		envID, serviceID, err := resolveServiceContext(processService)
		if err != nil {
			return err
		}
		client := api.NewClient()
		var procs []api.Process
		err = ui.RunWithSpinner("Loading processes...", func() error {
			var e error
			procs, e = client.ListProcesses(envID, serviceID)
			return e
		})
		if err != nil {
			return fmt.Errorf("failed to list processes: %w", err)
		}
		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(procs)
			return nil
		}
		if len(procs) == 0 {
			ui.PrintInfo("No processes")
			return nil
		}
		fmt.Println()
		table := ui.NewTable("Name", "Type", "Replicas", "Command")
		for _, p := range procs {
			table.AddRow(p.Name, p.Type, strconv.Itoa(p.InstanceCount), p.Command)
		}
		table.Print()
		fmt.Println()
		return nil
	},
}

// resolveProcess resolve a process by id OR name (case-insensitive).
func resolveProcess(client *api.Client, envID, serviceID, ref string) (*api.Process, error) {
	procs, err := client.ListProcesses(envID, serviceID)
	if err != nil {
		return nil, err
	}
	for i := range procs {
		if procs[i].ID == ref || strings.EqualFold(procs[i].Name, ref) {
			return &procs[i], nil
		}
	}
	return nil, fmt.Errorf("process %q not found", ref)
}

func init() {
	processCmd.Flags().StringVarP(&processService, "service", "s", "", "Service name, slug, or ID (overrides linked service)")
	rootCmd.AddCommand(processCmd)
}
