package cmd

import (
	"fmt"
	"strings"

	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

// targetAnchor diz de ONDE veio o alvo do comando. É a resposta única para as
// duas perguntas que antes cada helper respondia por conta própria: quem manda
// no workspace da sessão, e se o `environmentId`/`serviceId` do diretório
// descrevem o alvo.
type targetAnchor int

const (
	// anchorDirectory: o projeto linkado É o alvo. O pin do diretório manda, e
	// o contexto de serviço gravado nele é válido.
	anchorDirectory targetAnchor = iota
	// anchorFlag: -p nomeou OUTRO projeto. Nada do diretório se aplica.
	anchorFlag
)

// decideTargetAnchor resolve o anchor a partir do -p e do projeto linkado.
//
// Pura e sem rede de propósito: roda no caminho quente de todo comando de
// projeto, antes de qualquer chamada à API.
//
// O ref é comparado com o ID E com o nome do projeto linkado porque -p aceita
// os dois. `upuai redeploy -p upuai` de dentro do diretório do `upuai` nomeia o
// mesmo alvo que `upuai redeploy` — tratá-lo como flag desligaria o alinhamento
// de workspace sem que nada tivesse mudado de alvo.
func decideTargetAnchor(flagRef, linkedID, linkedName string) targetAnchor {
	if flagRef == "" {
		return anchorDirectory
	}
	if linkedID != "" && flagRef == linkedID {
		return anchorDirectory
	}
	if linkedName != "" && strings.EqualFold(flagRef, linkedName) {
		return anchorDirectory
	}
	return anchorFlag
}

// commandAnchor aplica decideTargetAnchor ao diretório atual.
func commandAnchor(cfg *config.ProjectConfig) targetAnchor {
	if cfg == nil {
		return decideTargetAnchor(flagProject, "", "")
	}
	return decideTargetAnchor(flagProject, cfg.ProjectID, cfg.ProjectName)
}

// workspacePinAction é o que o preflight deve fazer para alinhar a sessão ao
// workspace do diretório linkado.
type workspacePinAction int

const (
	// pinActionNone: nada a fazer — já alinhado, ou não há como decidir.
	pinActionNone workspacePinAction = iota
	// pinActionBackfill: o config foi gravado antes do pin existir; descobrir o
	// workspace dono do projeto e gravá-lo (trocando a sessão se divergir).
	pinActionBackfill
	// pinActionSwitch: o diretório pertence a outro workspace que o da sessão.
	pinActionSwitch
)

// decideWorkspacePin decide a ação do preflight. Pura de propósito: toda a
// política vive aqui, testável sem rede nem disco, e ensureLinkedWorkspace fica
// só com o I/O.
//
//	machineToken → none:      UPUAI_TOKEN é escopado a um workspace na criação e
//	                          a API recusa a troca. Um token de CI apontado pro
//	                          workspace errado é erro de configuração do pipeline,
//	                          não algo pra "consertar" em runtime.
//	activeID ""  → none:      sem sessão de usuário, ou token legado anterior ao
//	                          claim tenantId. Não há como comparar; seguir e
//	                          deixar a API decidir preserva o comportamento antigo.
//	linkedID ""  → backfill:  diretório linkado antes deste campo existir.
//	iguais       → none:      caso comum — zero rede.
func decideWorkspacePin(linkedID, activeID string, machineToken bool) workspacePinAction {
	if machineToken {
		return pinActionNone
	}
	if activeID == "" {
		return pinActionNone
	}
	if linkedID == "" {
		return pinActionBackfill
	}
	if linkedID == activeID {
		return pinActionNone
	}
	return pinActionSwitch
}

// workspacePinChecked memoiza o preflight dentro do processo. requireProject é
// chamado mais de uma vez por comando (resolveServiceContext o invoca de novo),
// e sem isso um único `upuai logs -s api` faria duas trocas de sessão.
var workspacePinChecked bool

// ensureLinkedWorkspace alinha o workspace da sessão ao do diretório linkado,
// antes que qualquer comando de projeto chame a API.
//
// Sem isso, operar um projeto de outro workspace devolve 404 — indistinguível de
// "não existe", porque o filtro de acesso da API é fail-closed por design. O
// usuário não tem como saber que o problema é workspace.
//
// O alinhamento nunca é COM -p: o pin é uma propriedade do diretório de
// trabalho, então o efeito é previsível e estável entre comandos. Trocar a
// sessão global por causa de uma flag ad-hoc deixaria o usuário em outro
// workspace depois que o comando terminasse.
//
// Mas também não é CONTRA -p. Quando a flag nomeia outro projeto, o diretório
// deixa de ser o alvo e o pin dele não tem autoridade nenhuma sobre a sessão —
// aplicá-lo assim mesmo criava um ciclo fechado: o comando errava, sugeria
// `upuai workspace switch <ws>`, e a invocação seguinte desfazia a troca antes
// de tentar de novo. Conselho impossível de seguir de dentro de qualquer
// diretório linkado, que é justamente de onde -p é usado.
func ensureLinkedWorkspace() error {
	if workspacePinChecked {
		return nil
	}
	workspacePinChecked = true

	cfg, _ := config.LoadProjectConfig()
	if cfg == nil || cfg.ProjectID == "" {
		return nil
	}
	if commandAnchor(cfg) == anchorFlag {
		return nil
	}

	activeID, activeName := "", ""
	if claims, _ := activeWorkspace(); claims != nil {
		activeID, activeName = claims.TenantID, claims.TenantName
	}

	switch decideWorkspacePin(cfg.WorkspaceID, activeID, config.MachineTokenFromEnv() != "") {
	case pinActionNone:
		return nil
	case pinActionBackfill:
		return backfillWorkspacePin(cfg.ProjectID, activeID)
	case pinActionSwitch:
		return switchToLinkedWorkspace(cfg.WorkspaceID, cfg.WorkspaceName, activeName)
	}
	return nil
}

// backfillWorkspacePin descobre o workspace dono de um projeto linkado antes do
// pin existir, grava no config e alinha a sessão se necessário.
//
// A descoberta é best-effort: um projeto apagado, ou cujo acesso foi revogado,
// responde 404 aqui exatamente como responderia no comando seguinte. Falhar o
// comando por causa do backfill trocaria um erro específico (o do comando) por um
// genérico. O erro real chega logo depois, pela chamada que o usuário pediu.
func backfillWorkspacePin(projectID, activeID string) error {
	client := api.NewClient()
	resolved, err := client.ResolveProjectWorkspace(projectID)
	if err != nil {
		return nil
	}

	_ = config.UpdateProjectConfig(func(c *config.ProjectConfig) {
		c.WorkspaceID = resolved.TenantID
		c.WorkspaceName = resolved.TenantName
	})

	if resolved.TenantID == activeID {
		return nil
	}
	return switchToLinkedWorkspace(resolved.TenantID, resolved.TenantName, "")
}

// worthAskingAboutWorkspace diz se um erro da API pode significar "esse recurso
// está fora do workspace ativo", justificando uma consulta a /tenant/resolve.
//
// A API usa DOIS códigos para isso, e a diferença não é arbitrária:
//   - 403 — assertProjectAccess (lib/tenant-guard.ts) confirmou que o projeto
//     existe e pertence a outro tenant.
//   - 404 — o projeto não existe, OU o endpoint depende só do filtro Prisma
//     (fail-closed), que torna "fora do escopo" indistinguível de "inexistente".
//
// Rede fora, 5xx e 400 nunca são convite pra trocar de sessão. O status só decide
// se VALE perguntar; quem responde é /tenant/resolve.
func worthAskingAboutWorkspace(status int) bool {
	return status == 403 || status == 404
}

// adoptWorkspaceForProject move a sessão para o workspace dono de projectID,
// quando o projeto está fora do workspace ativo mas dentro de outro do usuário.
// Devolve false (sem erro) quando não há o que adotar — projeto inexistente, de
// terceiros, ou API fora — para o caller propagar o erro original.
//
// Existe para `upuai link <id>`, o caminho mais provável de esbarrar no problema:
// copia-se o ID do dashboard de um workspace e tenta-se linkar de outro. Aqui a
// troca é coerente com a intenção — `link` É o ato de declarar o pin do
// diretório, então adotar o workspace é o que o comando significa.
func adoptWorkspaceForProject(client *api.Client, projectID string, cause error) (bool, error) {
	if !worthAskingAboutWorkspace(api.StatusCode(cause)) {
		return false, nil
	}
	if config.MachineTokenFromEnv() != "" {
		return false, nil
	}

	resolved, err := client.ResolveProjectWorkspace(projectID)
	if err != nil {
		return false, nil
	}

	activeID := ""
	if claims, _ := activeWorkspace(); claims != nil {
		activeID = claims.TenantID
	}
	if resolved.TenantID == activeID {
		return false, nil
	}

	if err := switchWorkspace(client, resolved.TenantID); err != nil {
		return false, fmt.Errorf("project %q belongs to workspace %q, but the session could not switch to it: %w",
			resolved.ProjectName, resolved.TenantName, err)
	}
	ui.PrintNotice("workspace: " + resolved.TenantName)
	return true, nil
}

// switchToLinkedWorkspace troca a sessão para o workspace do diretório e informa
// a troca. O aviso vai pro stderr (ui.PrintNotice) — é diagnóstico, não payload,
// e não pode sujar `upuai ... -o json | jq`.
func switchToLinkedWorkspace(workspaceID, workspaceName, fromName string) error {
	label := workspaceName
	if label == "" {
		label = workspaceID
	}

	if err := switchWorkspace(api.NewClient(), workspaceID); err != nil {
		return fmt.Errorf("this directory is linked to workspace %q, but the session could not switch to it: %w", label, err)
	}

	if fromName != "" {
		ui.PrintNotice(fmt.Sprintf("workspace: %s (was %s)", label, fromName))
		return nil
	}
	ui.PrintNotice("workspace: " + label)
	return nil
}
