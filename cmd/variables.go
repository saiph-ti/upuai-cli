package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var variablesService string

var variablesCmd = &cobra.Command{
	Use:     "variables",
	Aliases: []string{"vars", "variable"},
	Short:   "Manage environment variables",
	Long: `Manage environment variables for the linked service (or another service via -s).

Examples:
  upuai variables list
  upuai variables list -s api
  upuai variables set KEY=VALUE
  upuai variables set KEY1=VALUE1 KEY2=VALUE2
  upuai variables delete KEY`,
}

var variablesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List environment variables",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		envID, serviceID, err := resolveServiceContext(variablesService)
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
			return fmt.Errorf("failed to list variables: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(vars)
			return nil
		}

		if len(vars) == 0 {
			ui.PrintInfo("No variables configured")
			return nil
		}

		fmt.Println()
		table := ui.NewTable("Key", "Value", "Secret")
		for _, v := range vars {
			value := v.DisplayValue()
			if v.IsSecret {
				value = "********"
			}
			secret := "No"
			if v.IsSecret {
				secret = "Yes"
			}
			table.AddRow(v.Key, value, secret)
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

		envID, serviceID, err := resolveServiceContext(variablesService)
		if err != nil {
			return err
		}

		var vars []api.VariableInput
		for _, arg := range args {
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid format %q — use KEY=VALUE", arg)
			}
			vars = append(vars, api.VariableInput{
				Key:   parts[0],
				Value: parts[1],
			})
		}

		client := api.NewClient()

		var result []api.EnvVar
		err = ui.RunWithSpinner("Setting variables...", func() error {
			var setErr error
			result, setErr = client.SetVariables(envID, serviceID, vars)
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
			ui.PrintSuccess(fmt.Sprintf("Set %s", v.Key))
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

		envID, serviceID, err := resolveServiceContext(variablesService)
		if err != nil {
			return err
		}

		key := args[0]

		if !flagYes {
			confirmed, err := ui.Confirm(fmt.Sprintf("Delete variable %q?", key))
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
			return client.DeleteVariable(envID, serviceID, key)
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
	variablesCmd.AddCommand(variablesListCmd)
	variablesCmd.AddCommand(variablesSetCmd)
	variablesCmd.AddCommand(variablesDeleteCmd)
	rootCmd.AddCommand(variablesCmd)
}
