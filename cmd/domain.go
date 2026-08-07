package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var domainService string

var domainCmd = &cobra.Command{
	Use:     "domain",
	Aliases: []string{"domains"},
	Short:   "Manage custom domains",
	Long: `Manage custom domains for the linked service.

Examples:
  upuai domain list
  upuai domain list -s worker-api
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

		envID, serviceID, err := resolveServiceContext(domainService)
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
		table := ui.NewTable("Domain", "Type", "Status", "SSL", "ID")
		for _, d := range domains {
			ssl := d.SslStatus
			if ssl == "" {
				ssl = "-"
			}
			table.AddRow(d.Domain, d.Type, d.Status, ssl, d.ID)
		}
		table.Print()
		for _, d := range domains {
			if d.SslError != "" {
				ui.PrintWarning(fmt.Sprintf("%s: %s", d.Domain, d.SslError))
			}
		}
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

		envID, serviceID, err := resolveServiceContext(domainService)
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

		// Instruções de DNS: imprime os registros exatos que o usuário precisa
		// publicar (incl. o CNAME `_acme-challenge` de delegação em domínios
		// wildcard). Sem isso o usuário de CLI ficava cego — só a UI mostrava.
		instr, insErr := client.GetDNSInstructions(envID, serviceID, domain.ID)
		if insErr == nil && instr != nil && (len(instr.DNSRecords) > 0 || instr.TxtRecord != nil) {
			fmt.Println()
			ui.PrintInfo(fmt.Sprintf("Publique estes registros DNS para %s:", domain.Domain))
			records := instr.DNSRecords
			if instr.TxtRecord != nil {
				records = append(records, *instr.TxtRecord)
			}
			for _, r := range records {
				fmt.Println()
				ui.PrintKeyValue(
					"Tipo", r.Type,
					"Nome", r.Name,
					"Valor", r.Value,
					"TTL", fmt.Sprintf("%ds", r.TTL),
				)
				if hint := dnsNoteHint(r.Note); hint != "" {
					ui.PrintInfo(hint)
				}
			}
			fmt.Println()
		} else {
			fmt.Println()
			ui.PrintInfo("Configure your DNS to point to Upuai Cloud")
			fmt.Println()
		}

		return nil
	},
}

// dnsNoteHint traduz o campo `note` de um registro DNS numa dica curta pro
// usuário. Notes vêm de apps/shared/src/types/domain-types.ts (DnsRecordNote).
func dnsNoteHint(note string) string {
	switch note {
	case "acmeDelegation":
		return "Delegação do certificado (uma vez): permite emitir o TLS curinga sem nos dar acesso ao seu DNS. Mantenha DNS-only (nuvem cinza)."
	case "wildcardRoot":
		return "Registro curinga: encaminha qualquer subdomínio (slug.seu-dominio) para o serviço."
	case "apexStableIp":
		return "IP estável da Upuai (mudanças anunciadas com 30 dias de antecedência)."
	default:
		return ""
	}
}

var flagDomainPort int

var domainGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate an auto-generated domain (*.apps.upuai.cloud)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		envID, serviceID, err := resolveServiceContext(domainService)
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

		envID, serviceID, err := resolveServiceContext(domainService)
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
	domainCmd.PersistentFlags().StringVarP(&domainService, "service", "s", "", "Service name, slug, or ID (overrides linked service)")
	domainCmd.AddCommand(domainListCmd)
	domainCmd.AddCommand(domainAddCmd)
	domainCmd.AddCommand(domainGenerateCmd)
	domainCmd.AddCommand(domainDeleteCmd)
	rootCmd.AddCommand(domainCmd)
}
