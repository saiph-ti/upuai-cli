package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
)

func testWorkspaces() []api.Workspace {
	return []api.Workspace{
		{ID: "cmpersonal000000000000001", Name: "Gabriel Braga", Slug: "gabriel-braga-000001", Role: "OWNER", Active: true},
		{ID: "cmtai00000000000000000002", Name: "TAI Tecnologia", Slug: "tai", Role: "ADMIN", Active: true},
	}
}

func TestMatchWorkspaceRef(t *testing.T) {
	workspaces := testWorkspaces()
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{"exact id", "cmtai00000000000000000002", "cmtai00000000000000000002"},
		{"slug", "tai", "cmtai00000000000000000002"},
		{"slug case-insensitive", "TAI", "cmtai00000000000000000002"},
		{"name", "TAI Tecnologia", "cmtai00000000000000000002"},
		{"name case-insensitive", "gabriel braga", "cmpersonal000000000000001"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := matchWorkspaceRef(workspaces, tc.ref)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ID != tc.want {
				t.Fatalf("matchWorkspaceRef(%q) = %q, want %q", tc.ref, got.ID, tc.want)
			}
		})
	}
}

// Diferente de matchProjectRef, um ref desconhecido NÃO passa adiante: GET /tenant
// devolve todas as memberships sem paginação, então a lista local é completa e o
// erro pode ser específico. O erro precisa mostrar as opções — dizer só "não
// encontrado" deixaria o usuário adivinhando o slug.
func TestMatchWorkspaceRefUnknownListsOptions(t *testing.T) {
	_, err := matchWorkspaceRef(testWorkspaces(), "acme")
	if err == nil {
		t.Fatal("esperado erro para workspace desconhecido")
	}
	for _, want := range []string{"acme", "tai", "gabriel-braga-000001"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("erro %q não menciona %q", err.Error(), want)
		}
	}
}

func TestMatchWorkspaceRefAmbiguous(t *testing.T) {
	// Nome de workspace não é único (só o slug é) — dois "Acme" viram erro com
	// os IDs, nunca uma escolha silenciosa.
	workspaces := []api.Workspace{
		{ID: "id-alpha", Name: "Acme", Slug: "acme-1"},
		{ID: "id-beta", Name: "ACME", Slug: "acme-2"},
	}
	_, err := matchWorkspaceRef(workspaces, "acme")
	if err == nil {
		t.Fatal("esperado erro de ambiguidade")
	}
	for _, want := range []string{"ambiguous", "id-alpha", "id-beta"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("erro %q não contém %q", err.Error(), want)
		}
	}
}

// Slug é único por design (TENANT_SLUG_REGEX + unicidade na API), então um ref
// que casa com o slug de um e o nome de outro deve resolver pelo slug, sem
// virar ambiguidade.
func TestMatchWorkspaceRefIDBeatsName(t *testing.T) {
	workspaces := []api.Workspace{
		{ID: "tai", Name: "Outro", Slug: "outro"},
		{ID: "id-beta", Name: "tai", Slug: "tai-tec"},
	}
	got, err := matchWorkspaceRef(workspaces, "tai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "tai" {
		t.Fatalf("ID exato deve vencer match por nome, veio %q", got.ID)
	}
}

// Trava a semântica real da API, verificada em produção: GET /projects/:id de um
// projeto de outro workspace responde 403 ("Access denied to this project",
// lib/tenant-guard.ts:42), não 404. Um gate só de 404 fazia `upuai link <id>`
// desistir exatamente no caso que ele existe para resolver.
func TestWorthAskingAboutWorkspace(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{403, true},  // existe, mas em outro tenant (assertProjectAccess)
		{404, true},  // inexistente OU escondido pelo filtro Prisma fail-closed
		{401, false}, // sessão inválida — re-login resolve, trocar de workspace não
		{400, false},
		{500, false},
		{0, false}, // erro de rede/parse: não veio da API
	}
	for _, tc := range tests {
		if got := worthAskingAboutWorkspace(tc.status); got != tc.want {
			t.Fatalf("worthAskingAboutWorkspace(%d) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// Invariante de arquitetura: TODO funil que resolve contexto a partir do
// .upuai/config.json precisa passar pelo preflight de workspace.
//
// A primeira versão ancorou ensureLinkedWorkspace() só em requireProject(), e
// sete comandos (`ssh`, `run`, `shell`, `ps`, `config`, `scheduler`,
// `variables shared`) resolvem o serviço linkado via requireServiceConfig() sem
// nunca chamar requireProject() — passavam direto e batiam no 404 mudo que o pin
// existe pra evitar. O teste é estrutural porque a alternativa (exercitar os 7
// comandos) exigiria rede: aqui basta garantir que os dois funis chamam.
func TestWorkspacePreflightCoversBothContextFunnels(t *testing.T) {
	file := parseCmdFile(t, "root.go")

	for _, funnel := range []string{"requireProject", "requireServiceConfig"} {
		if findFuncDecl(file, funnel) == nil {
			t.Fatalf("%s() não existe mais em root.go — se foi renomeado, atualize este invariante", funnel)
		}
		if !reachesFunc(file, funnel, "ensureLinkedWorkspace", map[string]bool{}) {
			t.Fatalf("%s() não alcança ensureLinkedWorkspace() — comandos que passam por esse funil "+
				"vão operar no workspace errado e receber 404 indistinguível de \"não existe\"", funnel)
		}
	}
}

// TestPreflightConsultsTheAnchor é o segundo braço do invariante, e trava o fix
// do ciclo de workspace: o preflight só pode aplicar o pin do diretório depois
// de conferir que o diretório É o alvo. Sem essa consulta, `-p` de outro
// workspace volta a ser insatisfazível — o comando sugere `workspace switch` e
// a invocação seguinte desfaz a troca antes de tentar de novo.
//
// Estrutural porque o comportamento já tem teste próprio
// (TestEnsureLinkedWorkspaceYieldsToProjectFlag); aqui o alvo é o acoplamento,
// que sobrevive a reescritas do corpo.
func TestPreflightConsultsTheAnchor(t *testing.T) {
	file := parseCmdFile(t, "workspace_pin.go")
	if !reachesFunc(file, "ensureLinkedWorkspace", "commandAnchor", map[string]bool{}) {
		t.Fatal("ensureLinkedWorkspace() não consulta commandAnchor() — o pin do diretório " +
			"voltaria a atropelar -p, refazendo o ciclo de troca de workspace")
	}
}

func parseCmdFile(t *testing.T, name string) *ast.File {
	t.Helper()
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return file
}

func findFuncDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

// reachesFunc diz se fnName alcança target — direto, ou através de outra função
// declarada no mesmo arquivo. O passo transitivo é o que faz o invariante travar
// a garantia em vez da forma: extrair um helper (requireProject →
// resolveTargetProject) é refactor legítimo, e um teste que quebrasse aí
// treinaria o próximo leitor a afrouxá-lo.
func reachesFunc(file *ast.File, fnName, target string, seen map[string]bool) bool {
	if seen[fnName] {
		return false
	}
	seen[fnName] = true

	fn := findFuncDecl(file, fnName)
	if fn == nil {
		return false
	}

	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name == target || reachesFunc(file, ident.Name, target, seen) {
			found = true
			return false
		}
		return true
	})
	return found
}

func TestDecideWorkspacePin(t *testing.T) {
	tests := []struct {
		name         string
		linkedID     string
		activeID     string
		machineToken bool
		want         workspacePinAction
	}{
		{
			name:     "aligned is a no-op (hot path, zero network)",
			linkedID: "ws-a", activeID: "ws-a", want: pinActionNone,
		},
		{
			name:     "diverged switches to the directory workspace",
			linkedID: "ws-a", activeID: "ws-b", want: pinActionSwitch,
		},
		{
			name:     "legacy config without pin is backfilled",
			linkedID: "", activeID: "ws-b", want: pinActionBackfill,
		},
		{
			// UPUAI_TOKEN é escopado a um workspace na criação e a API recusa a
			// troca (requireUserPrincipal). Tentar seria 403 garantido.
			name:     "machine token never switches",
			linkedID: "ws-a", activeID: "ws-b", machineToken: true, want: pinActionNone,
		},
		{
			name:     "machine token skips backfill too",
			linkedID: "", activeID: "ws-b", machineToken: true, want: pinActionNone,
		},
		{
			// Token legado, anterior ao claim tenantId: não há com o que comparar.
			// Seguir preserva o comportamento antigo em vez de inventar uma troca.
			name:     "no active claim is a no-op",
			linkedID: "ws-a", activeID: "", want: pinActionNone,
		},
		{
			name:     "no claim and no pin is a no-op",
			linkedID: "", activeID: "", want: pinActionNone,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decideWorkspacePin(tc.linkedID, tc.activeID, tc.machineToken)
			if got != tc.want {
				t.Fatalf("decideWorkspacePin(%q, %q, %v) = %v, want %v",
					tc.linkedID, tc.activeID, tc.machineToken, got, tc.want)
			}
		})
	}
}
