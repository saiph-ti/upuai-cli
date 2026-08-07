package cmd

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
)

func TestDecideTargetAnchor(t *testing.T) {
	tests := []struct {
		name       string
		flagRef    string
		linkedID   string
		linkedName string
		want       targetAnchor
	}{
		{
			name:    "sem -p o diretório é o alvo (caminho quente)",
			flagRef: "", linkedID: "proj-a", linkedName: "upuai",
			want: anchorDirectory,
		},
		{
			name:    "-p com o ID do projeto linkado ainda é o diretório",
			flagRef: "proj-a", linkedID: "proj-a", linkedName: "upuai",
			want: anchorDirectory,
		},
		{
			// -p aceita nome; nomear o próprio projeto não muda o alvo, então
			// desligar o pin aqui seria perder o alinhamento à toa.
			name:    "-p com o NOME do projeto linkado ainda é o diretório",
			flagRef: "UPUAI", linkedID: "proj-a", linkedName: "upuai",
			want: anchorDirectory,
		},
		{
			name:    "-p com outro projeto: o pin do diretório não se aplica",
			flagRef: "proj-b", linkedID: "proj-a", linkedName: "upuai",
			want: anchorFlag,
		},
		{
			name:    "-p sem diretório linkado é anchor de flag",
			flagRef: "proj-b", linkedID: "", linkedName: "",
			want: anchorFlag,
		},
		{
			// Config gravado antes do campo projectName existir: só o ID compara.
			name:    "config legado sem nome não confunde outro projeto com o linkado",
			flagRef: "outro", linkedID: "proj-a", linkedName: "",
			want: anchorFlag,
		},
		{
			// Nome vazio dos dois lados não pode virar match — senão qualquer -p
			// num diretório meio-preenchido seria tratado como o próprio projeto.
			name:    "nome vazio nunca casa",
			flagRef: "", linkedID: "", linkedName: "",
			want: anchorDirectory,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := decideTargetAnchor(tc.flagRef, tc.linkedID, tc.linkedName); got != tc.want {
				t.Fatalf("decideTargetAnchor(%q, %q, %q) = %v, quero %v",
					tc.flagRef, tc.linkedID, tc.linkedName, got, tc.want)
			}
		})
	}
}

// linkDirectory entra num diretório temporário linkado a cfg e restaura os
// globais de flag/memo no fim — eles são estado de processo, e um teste que os
// deixasse sujos contaminaria o próximo.
func linkDirectory(t *testing.T, cfg *config.ProjectConfig) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := config.SaveProjectConfig(cfg); err != nil {
		t.Fatalf("gravar project config: %v", err)
	}

	prevFlag, prevChecked := flagProject, workspacePinChecked
	prevDone, prevID, prevErr := targetProjectDone, targetProjectID, targetProjectErr
	t.Cleanup(func() {
		flagProject, workspacePinChecked = prevFlag, prevChecked
		targetProjectDone, targetProjectID, targetProjectErr = prevDone, prevID, prevErr
	})
}

func linkedConfig() *config.ProjectConfig {
	return &config.ProjectConfig{
		ProjectID:     "proj-a",
		ProjectName:   "upuai",
		WorkspaceID:   "ws-a",
		WorkspaceName: "Gabriel Braga",
		EnvironmentID: "env-a",
		ServiceID:     "svc-a",
		Environment:   "production",
	}
}

// TestRequireServiceConfigRefusesForeignProject trava o defeito que fazia
// `upuai down -p outro-projeto` parar o serviço DESTE diretório: -p era
// validado e depois ignorado, e o env/service vinham do .upuai/config.json.
func TestRequireServiceConfigRefusesForeignProject(t *testing.T) {
	linkDirectory(t, linkedConfig())
	flagProject = "proj-b"
	workspacePinChecked = true // preflight já resolvido: isola o que está sob teste

	_, _, err := requireServiceConfig()
	if err == nil {
		t.Fatal("requireServiceConfig aceitou o serviço do diretório com -p de outro projeto")
	}
	for _, want := range []string{"proj-b", "upuai", "-s"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("erro %q não menciona %q — precisa nomear os dois projetos e a saída", err, want)
		}
	}
}

func TestRequireServiceConfigAcceptsLinkedProject(t *testing.T) {
	linkDirectory(t, linkedConfig())
	workspacePinChecked = true

	for _, ref := range []string{"", "proj-a", "UPUAI"} {
		flagProject = ref
		envID, serviceID, err := requireServiceConfig()
		if err != nil {
			t.Fatalf("-p %q: %v", ref, err)
		}
		if envID != "env-a" || serviceID != "svc-a" {
			t.Fatalf("-p %q: (%s, %s), quero (env-a, svc-a)", ref, envID, serviceID)
		}
	}
}

func TestLinkedServiceForTargetDropsServiceOfAnotherProject(t *testing.T) {
	linkDirectory(t, linkedConfig())

	flagProject = ""
	if got := linkedServiceForTarget(); got != "svc-a" {
		t.Fatalf("sem -p: %q, quero svc-a", got)
	}
	flagProject = "proj-b"
	if got := linkedServiceForTarget(); got != "" {
		t.Fatalf("com -p de outro projeto: %q, quero vazio — senão `deploy` monta par projeto/serviço cruzado", got)
	}
}

func TestShouldLinkNewServiceOnlyForTheDirectory(t *testing.T) {
	cfg := linkedConfig()
	cfg.ServiceID = ""
	linkDirectory(t, cfg)

	flagProject = ""
	if !shouldLinkNewService(cfg) {
		t.Fatal("diretório sem serviço linkado deveria adotar o recém-criado")
	}
	flagProject = "proj-b"
	if shouldLinkNewService(cfg) {
		t.Fatal("`add -p outro` repontou este diretório para um serviço que não é dele")
	}
}

// TestResolveEnvironmentIDIgnoresLinkedEnvOfAnotherProject trava o par cruzado:
// ambiente é filho de projeto, então o environmentId do diretório só serve ao
// projeto que o gravou. Sem isso, `-p outro -s api` montava serviço de um
// projeto com ambiente de outro, e a API só recusava no fim, como 404 mudo.
func TestResolveEnvironmentIDIgnoresLinkedEnvOfAnotherProject(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/proj-b/environments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"env-b","name":"production"}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPUAI_API_URL", srv.URL)
	t.Setenv(config.EnvTokenVar, "")
	linkDirectory(t, linkedConfig())

	store := config.NewCredentialStore()
	if err := store.Save(&config.Credentials{Token: "tok", ApiURL: srv.URL}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	client := api.NewClient()

	flagProject = ""
	got, err := resolveEnvironmentID(client, "proj-a")
	if err != nil || got != "env-a" {
		t.Fatalf("projeto do diretório: (%q, %v), quero (env-a, nil) — zero rede no caminho quente", got, err)
	}

	flagProject = "proj-b"
	got, err = resolveEnvironmentID(client, "proj-b")
	if err != nil {
		t.Fatalf("resolveEnvironmentID(proj-b): %v", err)
	}
	if got != "env-b" {
		t.Fatalf("ambiente = %q, quero env-b — env-a é de outro projeto", got)
	}
}

// jwtWithTenant monta um token cujo payload decodifica no claim desejado.
// DecodeToken não valida assinatura (só lê claims para decidir localmente),
// então a terceira parte pode ser qualquer coisa.
func jwtWithTenant(tenantID, tenantName string) string {
	payload := `{"tenantId":"` + tenantID + `","tenantName":"` + tenantName + `"}`
	return "h." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".s"
}

// TestEnsureLinkedWorkspaceYieldsToProjectFlag é o teste do bug reportado: com
// -p nomeando outro projeto, o preflight NÃO pode arrastar a sessão de volta
// para o workspace do diretório. Era o que fechava o ciclo — o CLI mandava
// rodar `workspace switch <ws>` e desfazia a troca na invocação seguinte.
func TestEnsureLinkedWorkspaceYieldsToProjectFlag(t *testing.T) {
	switches := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/switch-tenant", func(w http.ResponseWriter, r *http.Request) {
		switches++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + jwtWithTenant("ws-a", "Gabriel Braga") + `","refreshToken":"refB"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Diretório pinado em ws-a, sessão em ws-b: divergência real, a mesma do
	// caso reportado (diretório pessoal, sessão na TAI).
	seed := func(t *testing.T) {
		t.Helper()
		store := config.NewCredentialStore()
		if err := store.Save(&config.Credentials{
			Token:        jwtWithTenant("ws-b", "TAI Tecnologia"),
			RefreshToken: "refA",
			ApiURL:       srv.URL,
		}); err != nil {
			t.Fatalf("seed credentials: %v", err)
		}
	}

	t.Run("sem -p o pin do diretório alinha a sessão", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("UPUAI_API_URL", srv.URL)
		t.Setenv(config.EnvTokenVar, "")
		linkDirectory(t, linkedConfig())
		seed(t)
		flagProject = ""
		workspacePinChecked = false

		if err := ensureLinkedWorkspace(); err != nil {
			t.Fatalf("ensureLinkedWorkspace: %v", err)
		}
		if switches != 1 {
			t.Fatalf("trocas de workspace = %d, quero 1 — o pin do diretório deve valer quando ele É o alvo", switches)
		}
	})

	t.Run("com -p de outro projeto o pin cede", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("UPUAI_API_URL", srv.URL)
		t.Setenv(config.EnvTokenVar, "")
		linkDirectory(t, linkedConfig())
		seed(t)
		flagProject = "proj-b"
		workspacePinChecked = false
		switches = 0

		if err := ensureLinkedWorkspace(); err != nil {
			t.Fatalf("ensureLinkedWorkspace: %v", err)
		}
		if switches != 0 {
			t.Fatalf("trocas de workspace = %d, quero 0 — arrastar a sessão de volta é o ciclo que o fix fecha", switches)
		}
	})
}
