package cmd

import (
	"fmt"
	"strings"

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

Adding an apex domain (example.com) also adds its www counterpart (and vice
versa); the counterpart redirects to the domain you added. Any domain of the
service can redirect to another one — the canonical host (www → apex or apex →
www) is your call.

Examples:
  upuai domain list
  upuai domain list -s worker-api
  upuai domain add my-app.example.com
  upuai domain add www.example.com --redirect-to example.com --status 301
  upuai domain update www.example.com --redirect-to example.com
  upuai domain update www.example.com --no-redirect
  upuai domain delete example.com`,
}

// formatDomainRedirect renders the REDIRECT column: "→ x.com (301)" or "-".
func formatDomainRedirect(d api.Domain) string {
	if d.RedirectTo == nil {
		return "-"
	}
	return fmt.Sprintf("→ %s (%d)", d.RedirectTo.Hostname, d.RedirectTo.Status)
}

// matchDomainRef resolves a domain reference (ID or hostname) to the domain ID.
// Hostname is unique platform-wide, so there is never an ambiguous match. Zero
// match returns ref unchanged — the API answers 404 and callers that already
// pass an ID keep working (same contract as matchProjectRef).
func matchDomainRef(domains []api.Domain, ref string) string {
	for _, d := range domains {
		if d.ID == ref {
			return d.ID
		}
	}
	for _, d := range domains {
		if strings.EqualFold(d.Domain, ref) {
			return d.ID
		}
	}
	return ref
}

func resolveDomainRef(client *api.Client, envID, serviceID, ref string) (string, error) {
	domains, err := client.ListDomains(envID, serviceID)
	if err != nil {
		return "", fmt.Errorf("resolve domain %q: %w", ref, err)
	}
	return matchDomainRef(domains, ref), nil
}

func validateRedirectStatus(status int) error {
	if status == 301 || status == 302 {
		return nil
	}
	return fmt.Errorf("--status must be 301 or 302 (got %d)", status)
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
		table := ui.NewTable("Domain", "Type", "Status", "SSL", "Redirect", "ID")
		for _, d := range domains {
			ssl := d.SslStatus
			if ssl == "" {
				ssl = "-"
			}
			table.AddRow(d.Domain, d.Type, d.Status, ssl, formatDomainRedirect(d), d.ID)
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

		if err := validateRedirectStatus(flagAddRedirectStatus); err != nil {
			return err
		}

		domainName := args[0]
		client := api.NewClient()
		req := api.AddDomainRequest{Hostname: domainName}
		if flagAddRedirectTo != "" {
			req.RedirectTo = flagAddRedirectTo
			req.RedirectStatus = flagAddRedirectStatus
		}

		var created *api.CreateDomainResponse
		err = ui.RunWithSpinner("Adding domain...", func() error {
			var addErr error
			created, addErr = client.AddDomain(envID, serviceID, req)
			return addErr
		})
		if err != nil {
			return fmt.Errorf("failed to add domain: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(created)
			return nil
		}

		domains := []api.Domain{created.Primary}
		if created.Sibling != nil {
			domains = append(domains, *created.Sibling)
		}

		fmt.Println()
		ui.PrintSuccess(fmt.Sprintf("Domain %s added", created.Primary.Domain))
		if created.Sibling != nil {
			ui.PrintInfo(fmt.Sprintf("Also added %s (apex/www pair — counts as one domain)", created.Sibling.Domain))
		}
		for _, d := range domains {
			fmt.Println()
			ui.PrintKeyValue(
				"Domain", d.Domain,
				"Type", d.Type,
				"Status", d.Status,
			)
			if d.RedirectTo != nil {
				ui.PrintInfo(fmt.Sprintf("Redirect: %s → %s (%d)", d.Domain, d.RedirectTo.Hostname, d.RedirectTo.Status))
			}
		}

		// Instruções de DNS dos DOIS lados do par: imprime os registros exatos
		// que o usuário precisa publicar (incl. o CNAME `_acme-challenge` de
		// delegação em domínios wildcard). Sem isso o usuário de CLI ficava
		// cego — só a UI mostrava.
		for _, d := range domains {
			printDNSInstructions(client, envID, serviceID, d)
		}

		return nil
	},
}

func printDNSInstructions(client *api.Client, envID, serviceID string, domain api.Domain) {
	instr, err := client.GetDNSInstructions(envID, serviceID, domain.ID)
	if err != nil || instr == nil || (len(instr.DNSRecords) == 0 && instr.TxtRecord == nil) {
		fmt.Println()
		ui.PrintInfo(fmt.Sprintf("Configure your DNS for %s to point to Upuai Cloud", domain.Domain))
		fmt.Println()
		return
	}

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

var (
	flagAddRedirectTo        string
	flagAddRedirectStatus    int
	flagUpdateRedirectTo     string
	flagUpdateNoRedirect     bool
	flagUpdateRedirectStatus int
)

var domainUpdateCmd = &cobra.Command{
	Use:   "update <domain|id>",
	Short: "Set, change or remove the redirect of a domain (canonical host)",
	Long: `Set, change or remove the redirect of a domain.

The redirect target must be another domain of the same service. It answers
301 (default, permanent — what search engines consolidate on) or 302, keeps
path and query string, and takes effect without a redeploy.

Examples:
  upuai domain update www.example.com --redirect-to example.com
  upuai domain update www.example.com --redirect-to example.com --status 302
  upuai domain update www.example.com --status 301          # change only the status
  upuai domain update www.example.com --no-redirect`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		statusChanged := cmd.Flags().Changed("status")
		if flagUpdateRedirectTo == "" && !flagUpdateNoRedirect && !statusChanged {
			return fmt.Errorf("nothing to update: pass --redirect-to <domain>, --status 301|302 or --no-redirect")
		}
		if statusChanged {
			if err := validateRedirectStatus(flagUpdateRedirectStatus); err != nil {
				return err
			}
		}

		envID, serviceID, err := resolveServiceContext(domainService)
		if err != nil {
			return err
		}

		client := api.NewClient()
		domainID, err := resolveDomainRef(client, envID, serviceID, args[0])
		if err != nil {
			return err
		}

		body := map[string]any{}
		switch {
		case flagUpdateNoRedirect:
			body["redirectTo"] = nil
		case flagUpdateRedirectTo != "":
			body["redirectTo"] = flagUpdateRedirectTo
			body["redirectStatus"] = flagUpdateRedirectStatus
		default:
			body["redirectStatus"] = flagUpdateRedirectStatus
		}

		var domain *api.Domain
		err = ui.RunWithSpinner("Updating domain...", func() error {
			var updErr error
			domain, updErr = client.UpdateDomain(envID, serviceID, domainID, body)
			return updErr
		})
		if err != nil {
			return fmt.Errorf("failed to update domain: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(domain)
			return nil
		}

		fmt.Println()
		if domain.RedirectTo == nil {
			ui.PrintSuccess(fmt.Sprintf("%s no longer redirects", domain.Domain))
		} else {
			ui.PrintSuccess(fmt.Sprintf("%s → %s (%d)", domain.Domain, domain.RedirectTo.Hostname, domain.RedirectTo.Status))
		}
		fmt.Println()

		return nil
	},
}

var domainDeleteCmd = &cobra.Command{
	Use:   "delete <domain|id>",
	Short: "Delete a domain (and its apex/www counterpart)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		envID, serviceID, err := resolveServiceContext(domainService)
		if err != nil {
			return err
		}

		client := api.NewClient()
		domainID, err := resolveDomainRef(client, envID, serviceID, args[0])
		if err != nil {
			return err
		}

		if !flagYes {
			confirmed, err := ui.Confirm(fmt.Sprintf("Delete domain %s?", args[0]))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Delete cancelled")
				return nil
			}
		}

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
	domainAddCmd.Flags().StringVar(&flagAddRedirectTo, "redirect-to", "", "Redirect this domain to another domain of the service (e.g. the apex)")
	domainAddCmd.Flags().IntVar(&flagAddRedirectStatus, "status", 301, "Redirect status code: 301 (permanent) or 302")
	domainUpdateCmd.Flags().StringVar(&flagUpdateRedirectTo, "redirect-to", "", "Redirect this domain to another domain of the service")
	domainUpdateCmd.Flags().BoolVar(&flagUpdateNoRedirect, "no-redirect", false, "Remove the redirect (the domain serves the app again)")
	domainUpdateCmd.Flags().IntVar(&flagUpdateRedirectStatus, "status", 301, "Redirect status code: 301 (permanent) or 302")
	domainUpdateCmd.MarkFlagsMutuallyExclusive("redirect-to", "no-redirect")
	domainUpdateCmd.MarkFlagsMutuallyExclusive("status", "no-redirect")
	domainCmd.PersistentFlags().StringVarP(&domainService, "service", "s", "", "Service name, slug, or ID (overrides linked service)")
	domainCmd.AddCommand(domainListCmd)
	domainCmd.AddCommand(domainAddCmd)
	domainCmd.AddCommand(domainUpdateCmd)
	domainCmd.AddCommand(domainGenerateCmd)
	domainCmd.AddCommand(domainDeleteCmd)
	rootCmd.AddCommand(domainCmd)
}
