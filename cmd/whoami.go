package cmd

import (
	"fmt"

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

		// activeWorkspace() — e não DecodeToken(creds.Token) — porque sob
		// UPUAI_TOKEN as chamadas de API usam o machine token, que é opaco e pode
		// estar em OUTRO workspace. Ler o token de login ali reportava com
		// confiança um workspace que não é o que a sessão está usando: pior que
		// não reportar nada, porque agentes e CI tratam este campo como verdade.
		claims, _ := activeWorkspace()
		usingMachineToken := config.MachineTokenFromEnv() != ""

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
			//
			// Com machine token os campos ficam AUSENTES e `machineToken: true`
			// aparece no lugar: o workspace de um token opaco não é legível no
			// cliente. Omitir é a resposta honesta — emitir o workspace do login
			// armazenado seria pior que silêncio, porque parece autoritativo.
			if usingMachineToken {
				data["machineToken"] = true
			} else if claims != nil {
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
		if usingMachineToken {
			pairs = append(pairs, "Auth", "machine token ("+config.EnvTokenVar+")")
		} else if claims != nil {
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

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
