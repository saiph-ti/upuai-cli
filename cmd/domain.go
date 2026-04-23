package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var domainCmd = &cobra.Command{
	Use:     "domain",
	Aliases: []string{"domains"},
	Short:   "Manage custom domains",
	Long: `Manage custom domains for the linked service.

Examples:
  upuai domain list
  upuai domain add my-app.example.com
  upuai domain delete <domain-id>`,
}

var domainListCmd = &cobra.Command{
	Use:   "list",
	Short: "List domains",
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

		var domains []api.Domain
		err = ui.RunWithSpinner("Loading domains...", func() error {
			var fetchErr error
			domains, fetchErr = client.ListDomains(envID, serviceID)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list domains: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(domains)
			return nil
		}

		if len(domains) == 0 {
			ui.PrintInfo("No domains configured")
			return nil
		}

		fmt.Println()
		table := ui.NewTable("Domain", "Type", "Status", "ID")
		for _, d := range domains {
			table.AddRow(d.Domain, d.Type, d.Status, d.ID)
		}
		table.Print()
		fmt.Println()

		return nil
	},
}

var domainAddCmd = &cobra.Command{
	Use:   "add <domain>",
	Short: "Add a custom domain",
	Args:  cobra.ExactArgs(1),
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

		domainName := args[0]
		client := api.NewClient()

		var domain *api.Domain
		err = ui.RunWithSpinner("Adding domain...", func() error {
			var addErr error
			domain, addErr = client.AddDomain(envID, serviceID, domainName)
			return addErr
		})
		if err != nil {
			return fmt.Errorf("failed to add domain: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(domain)
			return nil
		}

		fmt.Println()
		ui.PrintSuccess(fmt.Sprintf("Domain %s added", domain.Domain))
		ui.PrintKeyValue(
			"Domain", domain.Domain,
			"Type", domain.Type,
			"Status", domain.Status,
		)
		fmt.Println()
		ui.PrintInfo("Configure your DNS to point to Upuai Cloud")
		fmt.Println()

		return nil
	},
}

var flagDomainPort int

var domainGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate an auto-generated domain (*.apps.upuai.cloud)",
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

		var domain *api.Domain
		err = ui.RunWithSpinner("Generating domain...", func() error {
			var genErr error
			domain, genErr = client.GenerateDomain(envID, serviceID, flagDomainPort)
			return genErr
		})
		if err != nil {
			return fmt.Errorf("failed to generate domain: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(domain)
			return nil
		}

		fmt.Println()
		ui.PrintSuccess(fmt.Sprintf("Domain generated: https://%s", domain.Domain))
		ui.PrintKeyValue(
			"Domain", domain.Domain,
			"Type", domain.Type,
			"Status", domain.Status,
		)
		fmt.Println()

		return nil
	},
}

var domainDeleteCmd = &cobra.Command{
	Use:   "delete <domain-id>",
	Short: "Delete a domain",
	Args:  cobra.ExactArgs(1),
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

		domainID := args[0]

		if !flagYes {
			confirmed, err := ui.Confirm(fmt.Sprintf("Delete domain %s?", domainID))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Delete cancelled")
				return nil
			}
		}

		client := api.NewClient()

		err = ui.RunWithSpinner("Deleting domain...", func() error {
			return client.DeleteDomain(envID, serviceID, domainID)
		})
		if err != nil {
			return fmt.Errorf("failed to delete domain: %w", err)
		}

		ui.PrintSuccess("Domain deleted")
		return nil
	},
}

func init() {
	domainGenerateCmd.Flags().IntVar(&flagDomainPort, "port", 3000, "Target port the service listens on")
	domainCmd.AddCommand(domainListCmd)
	domainCmd.AddCommand(domainAddCmd)
	domainCmd.AddCommand(domainGenerateCmd)
	domainCmd.AddCommand(domainDeleteCmd)
	rootCmd.AddCommand(domainCmd)
}
