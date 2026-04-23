package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var stackCmd = &cobra.Command{
	Use:     "stack",
	Aliases: []string{"stacks"},
	Short:   "Manage deployed stack instances",
	Long: `Stack instances are provisioned multi-service apps (WordPress = wordpress +
MySQL + volume). Use 'upuai catalog deploy' to create a new stack.

Examples:
  upuai stack list
  upuai stack get <stackId>
  upuai stack delete <stackId>`,
}

var stackListCmd = &cobra.Command{
	Use:   "list",
	Short: "List stack instances in the current project",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		projectID, err := requireProject()
		if err != nil {
			return err
		}

		client := api.NewClient()
		var stacks []api.StackInstance
		err = ui.RunWithSpinner("Loading stacks...", func() error {
			var fetchErr error
			stacks, fetchErr = client.ListStackInstances(projectID)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list stacks: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(stacks)
			return nil
		}

		if len(stacks) == 0 {
			ui.PrintInfo("No stacks deployed yet — create one with 'upuai catalog deploy <slug>'")
			return nil
		}

		fmt.Println()
		table := ui.NewTable("ID", "Name", "Template", "Status", "Created")
		for _, s := range stacks {
			tmpl := fmt.Sprintf("%s@%s", s.TemplateSlug, s.TemplateVersion)
			table.AddRow(s.ID, s.Name, tmpl, s.Status, s.CreatedAt)
		}
		table.Print()
		fmt.Println()
		return nil
	},
}

var stackGetCmd = &cobra.Command{
	Use:   "get <stackId>",
	Short: "Show detailed status of a stack instance",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		projectID, err := requireProject()
		if err != nil {
			return err
		}
		stackID := args[0]

		client := api.NewClient()
		var inst *api.StackInstanceDetail
		err = ui.RunWithSpinner("Loading stack...", func() error {
			var fetchErr error
			inst, fetchErr = client.GetStackInstance(projectID, stackID)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to get stack: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(inst)
			return nil
		}

		fmt.Println()
		failure := ""
		if inst.FailureReason != nil {
			failure = *inst.FailureReason
		}
		ui.PrintKeyValue(
			"ID", inst.ID,
			"Name", inst.Name,
			"Template", fmt.Sprintf("%s@%s", inst.TemplateSlug, inst.TemplateVersion),
			"Status", inst.Status,
			"Environment", inst.EnvironmentID,
			"Created", inst.CreatedAt,
			"Failure", failure,
		)

		if len(inst.Services) > 0 {
			fmt.Println()
			fmt.Println(ui.Bold.Render("Services:"))
			fmt.Println()
			table := ui.NewTable("Node", "Role", "Order", "Service ID", "Name", "Type")
			for _, s := range inst.Services {
				table.AddRow(
					s.NodeName, s.Role, fmt.Sprintf("%d", s.DeployOrder),
					s.ServiceID, s.Service.Name, s.Service.Type,
				)
			}
			table.Print()
		}

		if len(inst.Outputs) > 0 {
			fmt.Println()
			fmt.Println(ui.Bold.Render("Outputs:"))
			for k, v := range inst.Outputs {
				fmt.Printf("  %s = %s\n", k, v)
			}
		}

		if len(inst.Inputs) > 0 {
			fmt.Println()
			fmt.Println(ui.Bold.Render("Inputs:"))
			for k, v := range inst.Inputs {
				fmt.Printf("  %s = %v\n", k, v)
			}
		}

		fmt.Println()
		return nil
	},
}

var flagStackDeleteYes bool

var stackDeleteCmd = &cobra.Command{
	Use:   "delete <stackId>",
	Short: "Delete a stack instance (cascade — removes all services)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		projectID, err := requireProject()
		if err != nil {
			return err
		}
		stackID := args[0]

		if !flagStackDeleteYes {
			ok, err := ui.Confirm(fmt.Sprintf("Delete stack %s and all its services?", stackID))
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}

		client := api.NewClient()
		err = ui.RunWithSpinner("Deleting stack...", func() error {
			return client.DeleteStack(projectID, stackID)
		})
		if err != nil {
			return fmt.Errorf("failed to delete stack: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(map[string]interface{}{"deleted": true, "stackId": stackID})
			return nil
		}
		ui.PrintSuccess(fmt.Sprintf("Stack %s deleted", stackID))
		return nil
	},
}

func init() {
	stackDeleteCmd.Flags().BoolVarP(&flagStackDeleteYes, "yes", "y", false, "Skip confirmation")
	stackCmd.AddCommand(stackListCmd)
	stackCmd.AddCommand(stackGetCmd)
	stackCmd.AddCommand(stackDeleteCmd)
	rootCmd.AddCommand(stackCmd)
}
