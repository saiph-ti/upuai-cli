package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var variablesSharedOrigin string

// variablesSharedCmd: vínculo opt-in de variáveis compartilhadas POR SERVIÇO.
// Diferente do flag `--shared` (que mira a CAMADA de ambiente em set/delete):
// aqui você escolhe QUAIS shared vars (projeto/ambiente) já definidas são
// injetadas NESTE serviço. Paridade com a seção "Compartilhadas" da aba de
// Variáveis na web.
var variablesSharedCmd = &cobra.Command{
	Use:   "shared",
	Short: "Choose which shared variables are injected into a service",
	Long: `Choose which project/environment shared variables are injected into a
specific service (opt-in binding — parity with the web "Shared" section).

Different from the --shared flag, which targets the environment LAYER when
creating/deleting vars. Here you pick, per service, which already-defined shared
variables this service receives on deploy.

Examples:
  upuai variables shared list -s api
  upuai variables shared enable DATABASE_URL REDIS_URL -s worker
  upuai variables shared disable DATABASE_URL -s site
  upuai variables shared enable PUBLIC_ID --origin project -s api`,
}

var variablesSharedListCmd = &cobra.Command{
	Use:   "list",
	Short: "List shared variables available to a service and whether each is enabled",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		envID, serviceID, err := resolveServiceContext(variablesService)
		if err != nil {
			return err
		}

		client := api.NewClient()
		var bindings []api.SharedVariableBinding
		err = ui.RunWithSpinner("Loading shared variables...", func() error {
			var e error
			bindings, e = client.ListServiceSharedVariables(envID, serviceID)
			return e
		})
		if err != nil {
			return fmt.Errorf("failed to list shared variables: %w", err)
		}

		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(bindings)
			return nil
		}
		if len(bindings) == 0 {
			ui.PrintInfo("No shared variables defined in this project/environment")
			return nil
		}

		fmt.Println()
		table := ui.NewTable("Key", "Origin", "Enabled", "Overridden")
		for _, b := range bindings {
			table.AddRow(b.Key, b.Origin, boolLabel(b.Bound), boolLabel(b.Overridden))
		}
		table.Print()
		fmt.Println()
		return nil
	},
}

var variablesSharedEnableCmd = &cobra.Command{
	Use:   "enable KEY [KEY...]",
	Short: "Inject shared variable(s) into this service",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSharedBinding(true, args)
	},
}

var variablesSharedDisableCmd = &cobra.Command{
	Use:   "disable KEY [KEY...]",
	Short: "Stop injecting shared variable(s) into this service",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSharedBinding(false, args)
	},
}

func runSharedBinding(bound bool, keys []string) error {
	if err := requireAuth(); err != nil {
		return err
	}
	envID, serviceID, err := resolveServiceContext(variablesService)
	if err != nil {
		return err
	}

	origin := strings.ToLower(strings.TrimSpace(variablesSharedOrigin))
	if origin != "" && origin != "project" && origin != "environment" {
		return fmt.Errorf("invalid --origin %q — use project|environment", variablesSharedOrigin)
	}

	client := api.NewClient()
	var available []api.SharedVariableBinding
	err = ui.RunWithSpinner("Loading shared variables...", func() error {
		var e error
		available, e = client.ListServiceSharedVariables(envID, serviceID)
		return e
	})
	if err != nil {
		return fmt.Errorf("failed to load shared variables: %w", err)
	}

	// Resolve todas as keys ANTES de mutar — assim um KEY inválido não deixa o
	// comando pela metade.
	matches := make([]*api.SharedVariableBinding, 0, len(keys))
	for _, key := range keys {
		m, err := resolveSharedVar(available, key, origin)
		if err != nil {
			return err
		}
		matches = append(matches, m)
	}

	for _, m := range matches {
		match := m
		err = ui.RunWithSpinner(fmt.Sprintf("Updating %s...", match.Key), func() error {
			return client.SetServiceSharedVariableBinding(envID, serviceID, match.ID, match.Origin, bound)
		})
		if err != nil {
			return fmt.Errorf("failed to update %s: %w", match.Key, err)
		}
		verb := "Enabled"
		if !bound {
			verb = "Disabled"
		}
		ui.PrintSuccess(fmt.Sprintf("%s %s [%s]", verb, match.Key, match.Origin))
	}
	ui.PrintInfo("Redeploy the service to apply these changes.")
	return nil
}

// resolveSharedVar casa KEY (case-insensitive) na lista disponível. Quando a
// mesma key existe nas DUAS camadas (projeto+ambiente), exige --origin.
func resolveSharedVar(available []api.SharedVariableBinding, key, origin string) (*api.SharedVariableBinding, error) {
	var matches []api.SharedVariableBinding
	for _, b := range available {
		if strings.EqualFold(b.Key, key) && (origin == "" || b.Origin == origin) {
			matches = append(matches, b)
		}
	}
	switch len(matches) {
	case 0:
		if origin != "" {
			return nil, fmt.Errorf("no shared variable %q with origin %q for this service", key, origin)
		}
		return nil, fmt.Errorf("no shared variable %q available — define it first (e.g. `upuai variables set %s=... --shared`)", key, key)
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("%q exists in both project and environment layers — pass --origin project|environment", key)
	}
}

func boolLabel(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func init() {
	variablesSharedEnableCmd.Flags().StringVar(&variablesSharedOrigin, "origin", "", "Disambiguate when a key exists in both layers: project|environment")
	variablesSharedDisableCmd.Flags().StringVar(&variablesSharedOrigin, "origin", "", "Disambiguate when a key exists in both layers: project|environment")
	variablesSharedCmd.AddCommand(variablesSharedListCmd)
	variablesSharedCmd.AddCommand(variablesSharedEnableCmd)
	variablesSharedCmd.AddCommand(variablesSharedDisableCmd)
	variablesCmd.AddCommand(variablesSharedCmd)
}
