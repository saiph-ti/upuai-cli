package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var environmentCmd = &cobra.Command{
	Use:     "environment",
	Aliases: []string{"env"},
	Short:   "Manage environments",
	Long: `Manage project environments.

Without a subcommand, interactively switch the active environment.

Examples:
  upuai env                    # interactive switch
  upuai env list               # list environments
  upuai env switch production  # switch to env by name
  upuai env new preview        # create new environment
  upuai env delete preview     # delete environment`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default: interactive switch
		if err := requireAuth(); err != nil {
			return err
		}

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		client := api.NewClient()

		var environments []api.Environment
		err = ui.RunWithSpinner("Loading environments...", func() error {
			var fetchErr error
			environments, fetchErr = client.ListEnvironments(projectID)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list environments: %w", err)
		}

		if len(environments) == 0 {
			ui.PrintInfo("No environments found")
			return nil
		}

		names := make([]string, len(environments))
		for i, e := range environments {
			names[i] = e.Name
		}

		selected, err := ui.SelectOne("Switch to environment:", names)
		if err != nil {
			return err
		}

		return switchToEnvironment(environments, selected)
	},
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List environments",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		client := api.NewClient()

		var environments []api.Environment
		err = ui.RunWithSpinner("Loading environments...", func() error {
			var fetchErr error
			environments, fetchErr = client.ListEnvironments(projectID)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list environments: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(environments)
			return nil
		}

		if len(environments) == 0 {
			ui.PrintInfo("No environments found")
			return nil
		}

		cfg, _ := config.LoadProjectConfig()
		currentEnvID := ""
		if cfg != nil {
			currentEnvID = cfg.EnvironmentID
		}

		fmt.Println()
		table := ui.NewTable("Name", "ID", "Active", "Ephemeral")
		for _, e := range environments {
			active := ""
			if e.ID == currentEnvID {
				active = "●"
			}
			ephemeral := "No"
			if e.IsEphemeral {
				ephemeral = "Yes"
			}
			table.AddRow(e.Name, e.ID, active, ephemeral)
		}
		table.Print()
		fmt.Println()

		return nil
	},
}

var envSwitchCmd = &cobra.Command{
	Use:   "switch <name>",
	Short: "Switch active environment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		client := api.NewClient()

		var environments []api.Environment
		err = ui.RunWithSpinner("Loading environments...", func() error {
			var fetchErr error
			environments, fetchErr = client.ListEnvironments(projectID)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list environments: %w", err)
		}

		return switchToEnvironment(environments, args[0])
	},
}

var envNewCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Create a new environment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		client := api.NewClient()

		var env *api.Environment
		err = ui.RunWithSpinner("Creating environment...", func() error {
			var createErr error
			env, createErr = client.CreateEnvironment(projectID, &api.CreateEnvironmentRequest{
				Name: args[0],
			})
			return createErr
		})
		if err != nil {
			return fmt.Errorf("failed to create environment: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(env)
			return nil
		}

		ui.PrintSuccess(fmt.Sprintf("Environment %s created", env.Name))
		ui.PrintKeyValue("ID", env.ID, "Name", env.Name)

		return nil
	},
}

var envDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete an environment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		client := api.NewClient()
		envName := args[0]

		var environments []api.Environment
		err = ui.RunWithSpinner("Loading environments...", func() error {
			var fetchErr error
			environments, fetchErr = client.ListEnvironments(projectID)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list environments: %w", err)
		}

		var targetEnv *api.Environment
		for i, e := range environments {
			if e.Name == envName {
				targetEnv = &environments[i]
				break
			}
		}
		if targetEnv == nil {
			return fmt.Errorf("environment %q not found", envName)
		}

		if !flagYes {
			confirmed, err := ui.Confirm(fmt.Sprintf("Delete environment %q?", envName))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Delete cancelled")
				return nil
			}
		}

		err = ui.RunWithSpinner("Deleting environment...", func() error {
			return client.DeleteEnvironment(projectID, targetEnv.ID)
		})
		if err != nil {
			return fmt.Errorf("failed to delete environment: %w", err)
		}

		ui.PrintSuccess(fmt.Sprintf("Environment %s deleted", envName))
		return nil
	},
}

func switchToEnvironment(environments []api.Environment, name string) error {
	var targetEnv *api.Environment
	for i, e := range environments {
		if e.Name == name {
			targetEnv = &environments[i]
			break
		}
	}
	if targetEnv == nil {
		return fmt.Errorf("environment %q not found", name)
	}

	cfg, _ := config.LoadProjectConfig()
	if cfg == nil {
		return fmt.Errorf("no project config found")
	}

	cfg.EnvironmentID = targetEnv.ID
	cfg.Environment = targetEnv.Name

	if err := config.SaveProjectConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	ui.PrintSuccess(fmt.Sprintf("Switched to %s", targetEnv.Name))
	return nil
}

func init() {
	environmentCmd.AddCommand(envListCmd)
	environmentCmd.AddCommand(envSwitchCmd)
	environmentCmd.AddCommand(envNewCmd)
	environmentCmd.AddCommand(envDeleteCmd)
	rootCmd.AddCommand(environmentCmd)
}
