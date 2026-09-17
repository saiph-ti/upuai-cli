package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	tokenCreateName    string
	tokenCreateScopes  []string
	tokenCreateProject string
	tokenCreateExpires int
)

var tokenCmd = &cobra.Command{
	Use:     "token",
	Aliases: []string{"tokens"},
	Short:   "Manage scoped API tokens for CI/automation",
	Long: `Create, list and revoke scoped, revocable API tokens for non-interactive use.

Set a token in the UPUAI_TOKEN environment variable to authenticate the CLI
without an interactive login — ideal for CI/CD:

    export UPUAI_TOKEN=upua_...
    upuai up`,
}

var tokenCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a scoped API token (secret shown once)",
	Long: `Create a scoped, revocable machine token bound to your tenant (optionally a
single project). The secret is displayed ONCE — store it immediately.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		if strings.TrimSpace(tokenCreateName) == "" {
			return fmt.Errorf("--name is required")
		}
		scopes, err := normalizeTokenScopes(tokenCreateScopes)
		if err != nil {
			return err
		}

		client := api.NewClient()
		var result *api.ApiToken
		err = ui.RunWithSpinner("Creating token...", func() error {
			var apiErr error
			result, apiErr = client.CreateToken(api.CreateTokenRequest{
				Name:          tokenCreateName,
				Scopes:        scopes,
				ProjectID:     tokenCreateProject,
				ExpiresInDays: tokenCreateExpires,
			})
			return apiErr
		})
		if err != nil {
			return fmt.Errorf("failed to create token: %w", err)
		}

		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(result)
			return nil
		}

		ui.PrintSuccess("API token created")
		ui.PrintKeyValue("Name", result.Name, "Scopes", strings.Join(result.Scopes, ", "))
		fmt.Println()
		ui.PrintWarning("Copy this token now — it will not be shown again:")
		fmt.Println(result.Token)
		fmt.Println()
		ui.PrintInfo(fmt.Sprintf("Use it non-interactively:  export %s=%s", config.EnvTokenVar, result.Token))
		return nil
	},
}

var tokenListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List API tokens",
	RunE: func(_ *cobra.Command, _ []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		client := api.NewClient()
		var tokens []api.ApiToken
		err := ui.RunWithSpinner("Loading tokens...", func() error {
			var apiErr error
			tokens, apiErr = client.ListTokens()
			return apiErr
		})
		if err != nil {
			return fmt.Errorf("failed to list tokens: %w", err)
		}

		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(tokens)
			return nil
		}

		if len(tokens) == 0 {
			ui.PrintInfo("No API tokens yet. Create one with `upuai token create --name <name>`.")
			return nil
		}

		table := ui.NewTable("ID", "NAME", "PREFIX", "SCOPES", "STATUS", "LAST USED")
		for _, t := range tokens {
			table.AddRow(t.ID, t.Name, t.Prefix, strings.Join(t.Scopes, ","), tokenStatus(t, time.Now()), orDash(t.LastUsedAt))
		}
		table.Print()
		return nil
	},
}

var tokenRevokeCmd = &cobra.Command{
	Use:   "revoke <token-id>",
	Short: "Revoke an API token",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		id := args[0]
		if !flagYes {
			ok, err := ui.Confirm(fmt.Sprintf("Revoke token %s? Any CI using it will stop working.", id))
			if err != nil {
				return err
			}
			if !ok {
				ui.PrintInfo("Aborted.")
				return nil
			}
		}
		client := api.NewClient()
		err := ui.RunWithSpinner("Revoking token...", func() error {
			return client.RevokeToken(id)
		})
		if err != nil {
			return fmt.Errorf("failed to revoke token: %w", err)
		}
		ui.PrintSuccess("Token revoked")
		return nil
	},
}

func normalizeTokenScopes(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("at least one --scope is required (read, deploy)")
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		norm := strings.ToUpper(strings.TrimSpace(s))
		if norm != "READ" && norm != "DEPLOY" {
			return nil, fmt.Errorf("invalid scope %q (allowed: read, deploy)", s)
		}
		if !seen[norm] {
			out = append(out, norm)
			seen[norm] = true
		}
	}
	return out, nil
}

// tokenStatus espelha a recusa da API: revogado ou expirado não autentica. Um
// expiresAt ilegível é mostrado como está, sem afirmar que o token vale.
func tokenStatus(t api.ApiToken, now time.Time) string {
	if t.RevokedAt != nil {
		return "revoked"
	}
	if t.ExpiresAt != nil && *t.ExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339, *t.ExpiresAt)
		if err != nil {
			return "expires " + *t.ExpiresAt
		}
		if !expiresAt.After(now) {
			return "expired"
		}
	}
	return "active"
}

func orDash(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return *s
}

func init() {
	tokenCreateCmd.Flags().StringVar(&tokenCreateName, "name", "", "Human-readable name for the token (required)")
	tokenCreateCmd.Flags().StringSliceVar(&tokenCreateScopes, "scope", []string{"deploy"}, "Scope(s): read (read-only; no ssh, no bucket credentials) or deploy (read+write). Repeatable")
	tokenCreateCmd.Flags().StringVar(&tokenCreateProject, "project", "", "Narrow the token to a single project ID (default: tenant-wide)")
	tokenCreateCmd.Flags().IntVar(&tokenCreateExpires, "expires", 0, "Expire after N days (default: never; revoke to disable)")

	tokenCmd.AddCommand(tokenCreateCmd)
	tokenCmd.AddCommand(tokenListCmd)
	tokenCmd.AddCommand(tokenRevokeCmd)
	rootCmd.AddCommand(tokenCmd)
}
