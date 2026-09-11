package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var scaleService string

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
  upuai scale 3 -s worker-api  # scale another service of the project
  upuai scale web=2 worker=1   # scale process "web" to 2 and "worker" to 1`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		envID, serviceID, err := resolveServiceContext(scaleService)
		if err != nil {
			return err
		}

		client := api.NewClient()

		// Legacy form: a single bare integer scales the whole service.
		if len(args) == 1 {
			if _, convErr := strconv.Atoi(args[0]); convErr == nil {
				count, err := parseReplicaCount(args[0])
				if err != nil {
					return err
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
			count, err := parseReplicaCount(raw)
			if err != nil {
				return fmt.Errorf("process %q: %w", name, err)
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

// parseReplicaCount aceita só inteiros >= 1 — mesmo contrato da API
// (scaleInstanceSchema). Um serviço sempre roda ao menos 1 réplica; o que não
// deve rodar é excluído.
func parseReplicaCount(raw string) (int, error) {
	count, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid replica count %q — must be an integer of at least 1", raw)
	}
	if count < 1 {
		return 0, fmt.Errorf("invalid replica count %q — a service runs at least 1 replica; delete it with `upuai service delete <name>` if it should not run", raw)
	}
	return count, nil
}

func scaleWhole(client *api.Client, envID, serviceID string, count int) error {
	err := ui.RunWithSpinner(fmt.Sprintf("Scaling to %d replica(s)...", count), func() error {
		return client.ScaleInstance(envID, serviceID, "", count)
	})
	if err != nil {
		// Serviço multi-processo: a API recusa um scale sem alvo (PROCESS_REQUIRED)
		// e lista os processos na mensagem. Diz ao usuário a forma certa.
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.Code == "PROCESS_REQUIRED" {
			return fmt.Errorf("%s\nThis service runs multiple processes — scale one at a time with `upuai scale <process>=<count>` (list them with `upuai ps`)", apiErr.Message)
		}
		return fmt.Errorf("scale failed: %w", err)
	}
	ui.PrintSuccess(fmt.Sprintf("Scaled to %d replica(s)", count))
	return nil
}

func init() {
	scaleCmd.Flags().StringVarP(&scaleService, "service", "s", "", "Service name, slug, or ID (overrides linked service)")
	rootCmd.AddCommand(scaleCmd)
}
