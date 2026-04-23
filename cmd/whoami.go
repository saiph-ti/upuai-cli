package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	internalAuth "github.com/upuai-cloud/cli/internal/auth"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show current authenticated user",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		store := config.NewCredentialStore()
		creds, err := store.Load()
		if err != nil {
			return err
		}

		format := getOutputFormat()

		// Try to get fresh data from API
		client := api.NewClient()
		me, apiErr := client.GetMe()

		if format == ui.FormatJSON {
			data := map[string]any{
				"userId":   creds.User.UserID,
				"userName": creds.User.UserName,
				"email":    creds.User.Login,
				"apiUrl":   creds.ApiURL,
			}
			if me != nil {
				data["userId"] = me.ID
				data["userName"] = me.Name
				data["email"] = me.Email
			}
			// Add project context if available
			if cfg, _ := config.LoadProjectConfig(); cfg != nil {
				data["project"] = cfg.ProjectName
				data["environment"] = cfg.Environment
			}
			ui.PrintJSON(data)
			return nil
		}

		fmt.Println()
		ui.PrintBanner()

		userName := creds.User.UserName
		email := creds.User.Login
		if me != nil {
			userName = me.Name
			email = me.Email
		}

		pairs := []string{
			"User", userName,
			"Email", email,
			"API", creds.ApiURL,
		}

		// Token info
		claims, _ := internalAuth.DecodeToken(creds.Token)
		if claims != nil {
			if claims.TenantName != "" {
				pairs = append(pairs, "Organization", claims.TenantName)
			}
			if len(claims.Roles) > 0 {
				pairs = append(pairs, "Role", claims.Roles[0])
			}
		}

		// Project context
		if cfg, _ := config.LoadProjectConfig(); cfg != nil {
			pairs = append(pairs,
				"Project", cfg.ProjectName,
				"Environment", cfg.Environment,
			)
		}

		ui.PrintKeyValue(pairs...)
		fmt.Println()

		if apiErr != nil {
			ui.PrintWarning("Could not verify token with API (may be offline)")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
