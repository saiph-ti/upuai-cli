package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

// `upuai admin ...` — comandos administrativos da plataforma.
// Requer role ADMIN no JWT.
// Runbook da feature de storage: upuai-core/docs/runbooks/2026-04-24-storage-architecture.md

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Platform administration (requires ADMIN role)",
	Long: `Comandos administrativos da plataforma.

Exemplos:
  upuai admin storage            Dashboard de storage do cluster
  upuai admin storage pvcs       Lista PVCs com uso real`,
}

var adminStorageCmd = &cobra.Command{
	Use:   "storage",
	Short: "Cluster storage dashboard (tiers + alerts + summary)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		client := api.NewClient()

		var overview *api.AdminStorageOverview
		if err := ui.RunWithSpinner("Loading storage overview...", func() error {
			var apiErr error
			overview, apiErr = client.GetAdminStorageOverview()
			return apiErr
		}); err != nil {
			return fmt.Errorf("get storage overview: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(overview)
			return nil
		}

		printStorageOverview(overview)
		return nil
	},
}

var adminStoragePVCsCmd = &cobra.Command{
	Use:   "pvcs",
	Short: "List PVCs with real usage (sortable by usage, filter by namespace)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		ns, _ := cmd.Flags().GetString("namespace")

		client := api.NewClient()

		var pvcs []api.AdminStoragePVC
		if err := ui.RunWithSpinner("Loading PVCs...", func() error {
			var apiErr error
			pvcs, apiErr = client.ListAdminStoragePVCs(ns)
			return apiErr
		}); err != nil {
			return fmt.Errorf("list pvcs: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(pvcs)
			return nil
		}

		table := ui.NewTable("NAMESPACE", "PVC", "TIER", "STORAGECLASS", "DECLARED", "ACTUAL", "USAGE", "PHASE")
		for _, p := range pvcs {
			used := "—"
			pct := "—"
			if p.ActualBytes > 0 {
				used = humanBytes(p.ActualBytes)
				pct = fmt.Sprintf("%d%%", p.UsagePercent)
			}
			table.AddRow(
				p.Namespace,
				p.Name,
				p.Tier,
				p.StorageClass,
				humanBytes(p.DeclaredBytes),
				used,
				pct,
				p.Phase,
			)
		}
		table.Print()
		return nil
	},
}

func init() {
	adminStoragePVCsCmd.Flags().String("namespace", "", "Filter by namespace")
	adminStorageCmd.AddCommand(adminStoragePVCsCmd)
	adminCmd.AddCommand(adminStorageCmd)
	rootCmd.AddCommand(adminCmd)
}

// printStorageOverview renders the dashboard in a terminal-friendly form.
func printStorageOverview(o *api.AdminStorageOverview) {
	// Summary row
	fmt.Println()
	ui.PrintKeyValue(
		"Total capacity", humanBytes(o.Summary.TotalCapacityBytes),
		"Total used", humanBytes(o.Summary.TotalUsedBytes),
		"PVCs", fmt.Sprintf("%d", o.Summary.TotalPVCCount),
		"Unhealthy vols", fmt.Sprintf("%d", o.Summary.UnhealthyVolumes),
		"Active alerts", fmt.Sprintf("%d", o.Summary.ActiveAlerts),
		"Critical alerts", fmt.Sprintf("%d", o.Summary.CriticalAlerts),
	)

	// Tiers
	fmt.Println()
	fmt.Println("Tiers:")
	for _, t := range o.Tiers {
		fmt.Printf("  %s (%s)\n", t.Name, t.StorageClass)
		fmt.Printf("    %s / %s  (%d%%)  %d PVCs  healthy=%t\n",
			humanBytes(t.UsedBytes), humanBytes(t.CapacityBytes),
			t.UsagePercent, t.PVCCount, t.Healthy)
		if len(t.DiskTags) > 0 {
			fmt.Printf("    tags: %s\n", strings.Join(t.DiskTags, ","))
		}
	}

	// Alerts
	if len(o.Alerts) == 0 {
		fmt.Println()
		ui.PrintSuccess("No active storage alerts")
	} else {
		fmt.Println()
		fmt.Println("Active alerts:")
		for _, a := range o.Alerts {
			fmt.Printf("  [%s] %s — %s\n", strings.ToUpper(a.Severity), a.Name, a.Summary)
		}
	}
}

// humanBytes formats a byte count into a short human-readable string.
// Intentionally local to the CLI — orchestrator returns raw int64 for flexibility.
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
