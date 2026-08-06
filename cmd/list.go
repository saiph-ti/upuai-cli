package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		client := api.NewClient()

		var projects []api.Project
		err := ui.RunWithSpinner("Loading projects...", func() error {
			var fetchErr error
			projects, fetchErr = client.ListProjects()
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list projects: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(projects)
			return nil
		}

		// A listagem é sempre do workspace ATIVO — nomeá-lo evita a leitura errada
		// de "esses são todos os meus projetos" para quem participa de mais de um
		// workspace. Vale principalmente na lista vazia, onde o palpite natural é
		// "sumiram" em vez de "estou no workspace errado".
		workspaceName := ""
		if claims, _ := activeWorkspace(); claims != nil {
			workspaceName = claims.TenantName
		}

		if len(projects) == 0 {
			if workspaceName != "" {
				ui.PrintInfo("No projects in workspace " + ui.Accent.Render(workspaceName))
				ui.PrintInfo("Run 'upuai init' to create one, or 'upuai workspace switch' to change workspace")
				return nil
			}
			ui.PrintInfo("No projects found")
			ui.PrintInfo("Run 'upuai init' to create one")
			return nil
		}

		fmt.Println()
		if workspaceName != "" {
			ui.PrintKeyValue("Workspace", workspaceName)
			fmt.Println()
		}
		table := ui.NewTable("Name", "ID", "Status", "Created")
		for _, p := range projects {
			status := p.Status
			if status == "" {
				status = "—"
			}
			created := p.CreatedAt
			if created == "" {
				created = "—"
			}
			table.AddRow(p.Name, p.ID, status, created)
		}
		table.Print()
		fmt.Println()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
