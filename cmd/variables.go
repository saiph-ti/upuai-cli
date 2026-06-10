package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/envparse"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	variablesService string
	variablesScope   string
	variablesProject bool
	variablesShared  bool
)

// validEnvVarScopes mapeia o input do usuário (case-insensitive) pro valor
// canônico do enum EnvVarScope da API. Vazio = servidor mantém/aplica default.
var validEnvVarScopes = map[string]string{
	"both":    "BOTH",
	"runtime": "RUNTIME",
	"build":   "BUILD",
}

// varTarget descreve a camada-alvo das variáveis compartilhadas. Default =
// serviço; --shared = ambiente (todos os serviços do env); --project = global.
// Precedência na resolução do deploy: Serviço > Ambiente > Projeto.
type varTarget struct {
	layer     string // "service" | "environment" | "project"
	envID     string
	serviceID string
	projectID string
	label     string
}

func resolveVarTarget() (*varTarget, error) {
	if variablesProject && variablesShared {
		return nil, fmt.Errorf("use only one of --project or --shared")
	}
	switch {
	case variablesProject:
		pid, err := requireProject()
		if err != nil {
			return nil, err
		}
		return &varTarget{layer: "project", projectID: pid, label: "project (all environments)"}, nil
	case variablesShared:
		pid, err := requireProject()
		if err != nil {
			return nil, err
		}
		client := api.NewClient()
		envID, err := resolveEnvironmentID(client, pid)
		if err != nil {
			return nil, err
		}
		return &varTarget{layer: "environment", envID: envID, label: "environment (shared)"}, nil
	default:
		envID, serviceID, err := resolveServiceContext(variablesService)
		if err != nil {
			return nil, err
		}
		return &varTarget{layer: "service", envID: envID, serviceID: serviceID, label: "service"}, nil
	}
}

var variablesCmd = &cobra.Command{
	Use:     "variables",
	Aliases: []string{"vars", "variable"},
	Short:   "Manage environment variables",
	Long: `Manage environment variables in three layers (most specific wins on deploy:
service > environment > project):

  (default)    service-level — only this service
  --shared     environment-level — inherited by every service in the environment
  --project    project-level (global) — same value across all environments

Examples:
  upuai variables list
  upuai variables list --shared
  upuai variables set KEY=VALUE
  upuai variables set DATABASE_URL=... --shared            # shared by the env
  upuai variables set PUBLIC_ID=... --project              # global to the project
  upuai variables set DATABASE_URL=... --scope runtime     # not in build
  upuai variables set NPM_TOKEN=... --scope build          # not in runtime
  upuai variables delete KEY --shared`,
}

var variablesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List environment variables",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		t, err := resolveVarTarget()
		if err != nil {
			return err
		}

		client := api.NewClient()

		var vars []api.EnvVar
		err = ui.RunWithSpinner("Loading variables...", func() error {
			var fetchErr error
			switch t.layer {
			case "project":
				vars, fetchErr = client.ListProjectVariables(t.projectID)
			case "environment":
				vars, fetchErr = client.ListEnvironmentVariables(t.envID)
			default:
				vars, fetchErr = client.ListVariables(t.envID, t.serviceID)
			}
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list variables: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(vars)
			return nil
		}

		if len(vars) == 0 {
			ui.PrintInfo(fmt.Sprintf("No variables configured (%s)", t.label))
			return nil
		}

		fmt.Println()
		table := ui.NewTable("Key", "Value", "Secret", "Scope")
		for _, v := range vars {
			value := v.DisplayValue()
			if v.IsSecret {
				value = "********"
			}
			secret := "No"
			if v.IsSecret {
				secret = "Yes"
			}
			// GetScope: respostas legadas sem `scope` rendem BOTH (default da
			// API) — nunca uma célula vazia que pareça "sem escopo".
			table.AddRow(v.Key, value, secret, strings.ToLower(v.GetScope()))
		}
		table.Print()
		fmt.Println()

		return nil
	},
}

var variablesSetCmd = &cobra.Command{
	Use:   "set KEY=VALUE [KEY=VALUE...]",
	Short: "Set environment variables",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		t, err := resolveVarTarget()
		if err != nil {
			return err
		}

		scope := ""
		if variablesScope != "" {
			canonical, ok := validEnvVarScopes[strings.ToLower(variablesScope)]
			if !ok {
				return fmt.Errorf("invalid --scope %q — use both|runtime|build", variablesScope)
			}
			scope = canonical
		}

		var vars []api.VariableInput
		seen := map[string]int{}
		for _, arg := range args {
			parsed, ok := envparse.ParseSingle(arg)
			if !ok {
				return fmt.Errorf("invalid format %q — use KEY=VALUE", arg)
			}
			if prev, dup := seen[parsed.Key]; dup {
				return fmt.Errorf("duplicate key %q (already set at arg %d)", parsed.Key, prev+1)
			}
			seen[parsed.Key] = len(vars)
			vars = append(vars, api.VariableInput{
				Key:   parsed.Key,
				Value: parsed.Value,
				Scope: scope,
			})
		}

		client := api.NewClient()

		var result []api.EnvVar
		err = ui.RunWithSpinner("Setting variables...", func() error {
			var setErr error
			switch t.layer {
			case "project":
				result, setErr = client.SetProjectVariables(t.projectID, vars)
			case "environment":
				result, setErr = client.SetEnvironmentVariables(t.envID, vars)
			default:
				result, setErr = client.SetVariables(t.envID, t.serviceID, vars)
			}
			return setErr
		})
		if err != nil {
			return fmt.Errorf("failed to set variables: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(result)
			return nil
		}

		for _, v := range vars {
			suffix := fmt.Sprintf(" [%s]", t.label)
			if v.Scope != "" && v.Scope != "BOTH" {
				ui.PrintSuccess(fmt.Sprintf("Set %s (scope: %s)%s", v.Key, strings.ToLower(v.Scope), suffix))
			} else {
				ui.PrintSuccess(fmt.Sprintf("Set %s%s", v.Key, suffix))
			}
		}
		if t.layer != "service" {
			ui.PrintInfo("Redeploy the affected services to apply these changes.")
		}
		return nil
	},
}

var variablesDeleteCmd = &cobra.Command{
	Use:   "delete KEY",
	Short: "Delete an environment variable",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		t, err := resolveVarTarget()
		if err != nil {
			return err
		}

		key := args[0]

		if !flagYes {
			confirmed, err := ui.Confirm(fmt.Sprintf("Delete variable %q (%s)?", key, t.label))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Delete cancelled")
				return nil
			}
		}

		client := api.NewClient()

		err = ui.RunWithSpinner("Deleting variable...", func() error {
			switch t.layer {
			case "project":
				return client.DeleteProjectVariable(t.projectID, key)
			case "environment":
				return client.DeleteEnvironmentVariable(t.envID, key)
			default:
				return client.DeleteVariable(t.envID, t.serviceID, key)
			}
		})
		if err != nil {
			return fmt.Errorf("failed to delete variable: %w", err)
		}

		ui.PrintSuccess(fmt.Sprintf("Deleted %s", key))
		return nil
	},
}

func init() {
	variablesCmd.PersistentFlags().StringVarP(&variablesService, "service", "s", "", "Service name, slug, or ID (overrides linked service)")
	variablesCmd.PersistentFlags().BoolVar(&variablesProject, "project", false, "Target project-level variables (global to all environments)")
	variablesCmd.PersistentFlags().BoolVar(&variablesShared, "shared", false, "Target environment-level variables (shared by all services in the environment)")
	variablesSetCmd.Flags().StringVar(&variablesScope, "scope", "", "Injection phase: both (default) | runtime (not in build) | build (not in runtime)")
	variablesCmd.AddCommand(variablesListCmd)
	variablesCmd.AddCommand(variablesSetCmd)
	variablesCmd.AddCommand(variablesDeleteCmd)
	rootCmd.AddCommand(variablesCmd)
}
