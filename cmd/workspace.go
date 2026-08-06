package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	internalAuth "github.com/upuai-cloud/cli/internal/auth"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

// activeWorkspace devolve o workspace ativo da sessão, lido do claim `tenantId`
// do access token guardado.
//
// O token É a fonte única: a API reemite o claim a cada rotação preservando o
// workspace da linha de sessão (refresh_tokens.tenantId), então espelhar o valor
// no credentials.json só criaria drift entre dois lugares que precisam concordar.
//
// Decodificar um token expirado é seguro e proposital — o payload não é validado
// aqui (nunca confie nele para autorização, só para exibir/decidir localmente) e
// o refresh preserva o workspace, então o claim continua correto até o próximo
// comando renovar o token.
//
// Devolve (nil, nil) quando não há sessão de usuário: sem login, ou usando um
// machine token opaco (UPUAI_TOKEN), que não é JWT e não tem claims para ler.
func activeWorkspace() (*internalAuth.TokenClaims, error) {
	if config.MachineTokenFromEnv() != "" {
		return nil, nil
	}
	store := config.NewCredentialStore()
	creds, err := store.Load()
	if err != nil || creds == nil || creds.Token == "" {
		return nil, err
	}
	claims, err := internalAuth.DecodeToken(creds.Token)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// activeWorkspacePin devolve (id, name) do workspace ativo para gravar no
// .upuai/config.json. Ambos vazios quando não há claim legível (machine token,
// token legado): pin vazio é válido e o preflight cura depois via
// GET /tenant/resolve — melhor do que gravar um valor inventado.
func activeWorkspacePin() (id, name string) {
	claims, _ := activeWorkspace()
	if claims == nil {
		return "", ""
	}
	return claims.TenantID, claims.TenantName
}

var workspaceCmd = &cobra.Command{
	Use:     "workspace",
	Aliases: []string{"ws", "workspaces"},
	Short:   "Manage the active workspace",
	Long: `Manage the active workspace.

Your session is scoped to one workspace at a time. Projects, services, domains
and databases all live inside a workspace — switching changes what every other
command sees.

Linked directories remember their workspace, so entering a project of another
workspace switches automatically.`,
}

var workspaceListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List workspaces you belong to",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		client := api.NewClient()
		var workspaces []api.Workspace
		err := ui.RunWithSpinner("Loading workspaces...", func() error {
			var fetchErr error
			workspaces, fetchErr = client.ListWorkspaces()
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list workspaces: %w", err)
		}

		activeID := ""
		if claims, _ := activeWorkspace(); claims != nil {
			activeID = claims.TenantID
		}

		if getOutputFormat() == ui.FormatJSON {
			type workspaceJSON struct {
				api.Workspace
				Active bool `json:"activeSession"`
			}
			out := make([]workspaceJSON, 0, len(workspaces))
			for _, w := range workspaces {
				out = append(out, workspaceJSON{Workspace: w, Active: w.ID == activeID})
			}
			ui.PrintJSON(out)
			return nil
		}

		if len(workspaces) == 0 {
			ui.PrintInfo("No workspaces found")
			return nil
		}

		fmt.Println()
		table := ui.NewTable("Name", "Slug", "Role", "Plan", "Active")
		for _, w := range workspaces {
			active := ""
			if w.ID == activeID {
				active = "●"
			}
			name := w.Name
			// Um workspace desativado ainda aparece (a membership existe e a
			// sessão pode até estar nele), mas precisa ser visivelmente
			// diferente — trocar pra ele falha com TENANT_INACTIVE.
			if !w.Active {
				name += " (inactive)"
			}
			table.AddRow(name, w.Slug, w.Role, w.Plan, active)
		}
		table.Print()
		fmt.Println()

		return nil
	},
}

var workspaceCurrentCmd = &cobra.Command{
	Use:     "current",
	Aliases: []string{"show"},
	Short:   "Show the active workspace",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		if config.MachineTokenFromEnv() != "" {
			return fmt.Errorf("running with a machine token (%s) — its workspace is fixed at creation and not readable from the token; run 'upuai workspace list' with an interactive login to inspect workspaces", config.EnvTokenVar)
		}

		claims, err := activeWorkspace()
		if err != nil {
			return fmt.Errorf("read active workspace: %w", err)
		}
		if claims == nil || claims.TenantID == "" {
			return fmt.Errorf("session has no workspace — run 'upuai login' to re-authenticate")
		}

		role := ""
		if len(claims.Roles) > 0 {
			role = claims.Roles[0]
		}

		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(map[string]any{
				"workspaceId":   claims.TenantID,
				"workspaceName": claims.TenantName,
				"role":          role,
			})
			return nil
		}

		fmt.Println()
		pairs := []string{"Workspace", claims.TenantName, "ID", claims.TenantID}
		if role != "" {
			pairs = append(pairs, "Role", role)
		}
		ui.PrintKeyValue(pairs...)
		fmt.Println()

		return nil
	},
}

var workspaceSwitchCmd = &cobra.Command{
	Use:   "switch [workspace]",
	Short: "Switch the active workspace",
	Long: `Switch the active workspace.

Accepts a workspace ID, name or slug. Without an argument, prompts with the
workspaces you belong to.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		if err := assertSwitchableSession(); err != nil {
			return err
		}

		client := api.NewClient()
		var workspaces []api.Workspace
		err := ui.RunWithSpinner("Loading workspaces...", func() error {
			var fetchErr error
			workspaces, fetchErr = client.ListWorkspaces()
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list workspaces: %w", err)
		}
		if len(workspaces) == 0 {
			return fmt.Errorf("you don't belong to any workspace")
		}

		ref := ""
		if len(args) == 1 {
			ref = args[0]
		}

		var target *api.Workspace
		if ref == "" {
			target, err = selectWorkspace(workspaces)
		} else {
			target, err = matchWorkspaceRef(workspaces, ref)
		}
		if err != nil {
			return err
		}

		activeID := ""
		if claims, _ := activeWorkspace(); claims != nil {
			activeID = claims.TenantID
		}
		if target.ID == activeID {
			ui.PrintInfo(fmt.Sprintf("Already on %s", ui.Accent.Render(target.Name)))
			return nil
		}

		if err := switchWorkspace(client, target.ID); err != nil {
			return err
		}

		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(map[string]any{
				"workspaceId":   target.ID,
				"workspaceName": target.Name,
				"workspaceSlug": target.Slug,
			})
			return nil
		}
		ui.PrintSuccess("Switched to workspace " + ui.Accent.Render(target.Name))
		return nil
	},
}

// selectWorkspace prompts for one of the user's workspaces. Rótulo é "Name
// (slug)" porque nome não é único entre workspaces — dois "Produção" seriam
// indistinguíveis na lista.
func selectWorkspace(workspaces []api.Workspace) (*api.Workspace, error) {
	labels := make([]string, 0, len(workspaces))
	for _, w := range workspaces {
		labels = append(labels, fmt.Sprintf("%s (%s)", w.Name, w.Slug))
	}
	selected, err := ui.SelectOne("Select workspace:", labels)
	if err != nil {
		return nil, err
	}
	for i, label := range labels {
		if label == selected {
			return &workspaces[i], nil
		}
	}
	return nil, fmt.Errorf("no workspace selected")
}

// matchWorkspaceRef resolve uma referência (ID, nome ou slug, case-insensitive)
// contra as memberships do usuário. Espelha matchProjectRef com UMA diferença
// deliberada: zero match é erro aqui, listando os workspaces disponíveis. Lá o
// ref desconhecido segue pra API (um ID válido fora da página listada continua
// funcionando); aqui a lista É completa — GET /tenant devolve todas as
// memberships, sem paginação — então um ref sem match é inequivocamente um erro
// de digitação, e mandá-lo pra API só trocaria uma mensagem útil por um 403.
func matchWorkspaceRef(workspaces []api.Workspace, ref string) (*api.Workspace, error) {
	var byName []int
	for i, w := range workspaces {
		if w.ID == ref {
			return &workspaces[i], nil
		}
		if strings.EqualFold(w.Name, ref) || (w.Slug != "" && strings.EqualFold(w.Slug, ref)) {
			byName = append(byName, i)
		}
	}
	switch len(byName) {
	case 1:
		return &workspaces[byName[0]], nil
	case 0:
		var b strings.Builder
		for _, w := range workspaces {
			fmt.Fprintf(&b, "\n  • %s  (%s)", w.Name, w.Slug)
		}
		return nil, fmt.Errorf("workspace %q not found — you belong to:%s", ref, b.String())
	default:
		var b strings.Builder
		for _, i := range byName {
			fmt.Fprintf(&b, "\n  • %s  (%s)", workspaces[i].Name, workspaces[i].ID)
		}
		return nil, fmt.Errorf("workspace name %q is ambiguous (%d matches) — pass the workspace ID or slug instead:%s",
			ref, len(byName), b.String())
	}
}

// assertSwitchableSession rejeita as sessões que a API não deixa trocar de
// workspace, com a orientação certa para cada caso.
//
// Um machine token (UPUAI_TOKEN) é escopado a um workspace na criação e a API
// exige um principal humano para trocar (requireUserPrincipal). Sem esse guard o
// usuário levaria um 401/403 genérico e tentaria `upuai login`, que não resolve —
// a saída é emitir um token no workspace destino.
func assertSwitchableSession() error {
	if config.MachineTokenFromEnv() != "" {
		return fmt.Errorf("cannot switch workspace with a machine token: %s is scoped to a single workspace at creation\n  create a token inside the target workspace instead: upuai token create --name ci", config.EnvTokenVar)
	}
	return nil
}

// switchWorkspace troca a sessão para tenantID e persiste o novo par de tokens.
//
// Ordem crítica: a API revoga o refresh anterior ANTES de responder, então a
// resposta é a única cópia da sessão viva. Ela é gravada imediatamente, antes de
// qualquer output — um print no meio que falhasse (pipe fechado) deixaria a
// máquina com um refresh já revogado e forçaria login de novo.
func switchWorkspace(client *api.Client, tenantID string) error {
	if err := assertSwitchableSession(); err != nil {
		return err
	}

	store := config.NewCredentialStore()
	creds, err := store.Load()
	if err != nil {
		return fmt.Errorf("read credentials: %w", err)
	}
	if creds == nil {
		return errNotAuthenticated
	}

	var resp *api.LoginResponse
	err = ui.RunWithSpinner("Switching workspace...", func() error {
		var switchErr error
		resp, switchErr = client.SwitchWorkspace(tenantID, creds.RefreshToken)
		return switchErr
	})
	if err != nil {
		return explainSwitchError(err)
	}
	if resp.Token == "" {
		return fmt.Errorf("switch workspace: API returned an empty session")
	}

	creds.Token = resp.Token
	if resp.RefreshToken != "" {
		creds.RefreshToken = resp.RefreshToken
	}
	if err := store.Save(creds); err != nil {
		return fmt.Errorf("switch workspace: session issued but could not be saved: %w", err)
	}
	return nil
}

// explainSwitchError traduz os códigos estáveis do catálogo da API em orientação
// acionável. Ramifica por `code`, nunca por `message` — a mensagem é texto de UI,
// traduzível e reescrito sem aviso.
func explainSwitchError(err error) error {
	switch api.ErrorCode(err) {
	case "NOT_A_MEMBER":
		return fmt.Errorf("you are not a member of that workspace — run 'upuai workspace list' to see yours")
	case "IMPERSONATION_ACTIVE":
		return fmt.Errorf("exit impersonation on the dashboard before switching workspace")
	case "TENANT_INACTIVE":
		return fmt.Errorf("that workspace is deactivated — contact the workspace owner")
	default:
		return fmt.Errorf("failed to switch workspace: %w", err)
	}
}

func init() {
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceCurrentCmd)
	workspaceCmd.AddCommand(workspaceSwitchCmd)
	rootCmd.AddCommand(workspaceCmd)
}
