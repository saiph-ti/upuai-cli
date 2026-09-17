package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
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

		// Com UPUAI_TOKEN o principal de todos os comandos é o token, não o login
		// guardado: é ele que o whoami tem de descrever, e sem exigir
		// credentials.json (CI e servidores só têm o token).
		if config.MachineTokenFromEnv() != "" {
			return whoamiMachineToken(api.NewClient(), getOutputFormat())
		}

		store := config.NewCredentialStore()
		creds, err := store.Load()
		if err != nil {
			return err
		}
		// Load() devolve (nil, nil) se o arquivo sumiu — ex: `logout` concorrente
		// entre o requireAuth() acima e aqui. Sem esse guard, deref de creds.User
		// abaixo dá panic.
		if creds == nil {
			return fmt.Errorf("not logged in — run 'upuai login'")
		}

		format := getOutputFormat()

		// Try to get fresh data from API
		client := api.NewClient()
		me, apiErr := client.GetMe()

		claims, _ := activeWorkspace()

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
			// Workspace ativo da sessão. Sem isso, um agente ou pipeline rodando
			// `whoami -o json` não tinha como descobrir em qual workspace estava —
			// o dado só existia no ramo de tabela, para olho humano.
			if claims != nil {
				data["workspace"] = claims.TenantName
				data["workspaceId"] = claims.TenantID
				if len(claims.Roles) > 0 {
					data["role"] = claims.Roles[0]
				}
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

		// Token info. O rótulo é "Workspace" — o vocabulário do produto inteiro
		// (dashboard, docs, API). "Organization" só existia aqui e não casava com
		// nada que o usuário vê em outro lugar.
		if claims != nil {
			if claims.TenantName != "" {
				pairs = append(pairs, "Workspace", claims.TenantName)
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

// whoamiMachineToken descreve o token de UPUAI_TOKEN pela API. Um token recusado
// é erro (saída não-zero), não aviso: no CI é exatamente o que precisa parar o
// pipeline.
func whoamiMachineToken(client *api.Client, format ui.OutputFormat) error {
	self, err := client.GetSelfToken()
	if err != nil {
		if api.StatusCode(err) == 401 {
			return fmt.Errorf("%s was rejected by the API — it is revoked, expired or mistyped", config.EnvTokenVar)
		}
		return fmt.Errorf("could not verify %s with the API: %w", config.EnvTokenVar, err)
	}
	cfg, _ := config.LoadProjectConfig()

	if format == ui.FormatJSON {
		data := map[string]any{
			"machineToken":  true,
			"tokenId":       self.ID,
			"tokenName":     self.Name,
			"tokenPrefix":   self.Prefix,
			"scopes":        self.Scopes,
			"expiresAt":     self.ExpiresAt,
			"workspace":     self.Workspace.Name,
			"workspaceId":   self.Workspace.ID,
			"workspaceSlug": self.Workspace.Slug,
			"apiUrl":        config.GetAPIURL(),
		}
		if self.Project != nil {
			data["tokenProjectId"] = self.Project.ID
			data["tokenProjectName"] = self.Project.Name
		}
		if cfg != nil {
			data["project"] = cfg.ProjectName
			data["environment"] = cfg.Environment
		}
		ui.PrintJSON(data)
		return nil
	}

	tokenProject := "all projects in the workspace"
	if self.Project != nil {
		tokenProject = self.Project.Name
	}
	expires := "never"
	if self.ExpiresAt != nil && *self.ExpiresAt != "" {
		expires = *self.ExpiresAt
	}
	pairs := []string{
		"Auth", "machine token (" + config.EnvTokenVar + ")",
		"Token", self.Name + " (" + self.Prefix + ")",
		"Scopes", strings.ToLower(strings.Join(self.Scopes, ", ")),
		"Workspace", self.Workspace.Name,
		"Token project", tokenProject,
		"Expires", expires,
		"API", config.GetAPIURL(),
	}
	if cfg != nil {
		pairs = append(pairs, "Project", cfg.ProjectName, "Environment", cfg.Environment)
	}
	fmt.Println()
	ui.PrintKeyValue(pairs...)
	fmt.Println()
	return nil
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
