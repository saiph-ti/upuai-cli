package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
)

type sourcePatch struct {
	Path  string
	Type  string `json:"type"`
	Image string `json:"image"`
}

// newImageAPI serve ambientes e serviços do proj-a e registra cada PATCH de
// origem. O diretório fica linkado ao proj-a / production / svc-img.
func newImageAPI(t *testing.T, patches *[]sourcePatch) *api.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/proj-a/environments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"env-a","name":"production"},{"id":"env-b","name":"staging"}]`))
	})
	mux.HandleFunc("/projects/proj-a/services", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"svc-img","name":"mailpit","type":"docker_image","slug":"mailpit"},
			{"id":"svc-legacy","name":"proxy","type":"docker","slug":"proxy"},
			{"id":"svc-git","name":"web","type":"github","slug":"web"}
		]`))
	})
	mux.HandleFunc("/environments/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || !strings.HasSuffix(r.URL.Path, "/source") {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		patch := sourcePatch{Path: r.URL.Path}
		_ = json.Unmarshal(raw, &patch)
		*patches = append(*patches, patch)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPUAI_API_URL", srv.URL)
	t.Setenv(config.EnvTokenVar, "upua_ci_token")

	cfg := linkedConfig()
	cfg.ServiceID = "svc-img"
	linkDirectory(t, cfg)
	prevEnv := flagEnvironment
	t.Cleanup(func() { flagEnvironment = prevEnv })
	flagProject, flagEnvironment, workspacePinChecked = "", "", true
	return api.NewClient()
}

func TestSetServiceImageTargetsTheRequestedService(t *testing.T) {
	var patches []sourcePatch
	client := newImageAPI(t, &patches)

	id, err := setServiceImage(client, "proj-a", "proxy", "nginx:1.27")
	if err != nil || id != "svc-legacy" {
		t.Fatalf("-s proxy: (%q, %v), quero svc-legacy", id, err)
	}
	id, err = setServiceImage(client, "proj-a", "", "axllent/mailpit:v1.25.0")
	if err != nil || id != "svc-img" {
		t.Fatalf("serviço linkado: (%q, %v), quero svc-img", id, err)
	}

	want := []sourcePatch{
		{Path: "/environments/env-a/services/svc-legacy/source", Type: "DOCKER", Image: "nginx:1.27"},
		{Path: "/environments/env-a/services/svc-img/source", Type: "DOCKER", Image: "axllent/mailpit:v1.25.0"},
	}
	if len(patches) != len(want) {
		t.Fatalf("PATCH recebidos = %+v, quero %+v", patches, want)
	}
	for i := range want {
		if patches[i] != want[i] {
			t.Fatalf("PATCH[%d] = %+v, quero %+v", i, patches[i], want[i])
		}
	}
}

// A imagem vai para o mesmo ambiente do deploy: com -e, não para o do diretório.
func TestSetServiceImageFollowsEnvironmentFlag(t *testing.T) {
	var patches []sourcePatch
	client := newImageAPI(t, &patches)
	flagEnvironment = "staging"

	if _, err := setServiceImage(client, "proj-a", "", "axllent/mailpit:v1.25.0"); err != nil {
		t.Fatal(err)
	}
	if got := getEnvironment(); got != "staging" {
		t.Fatalf("getEnvironment() = %q, quero staging (o deploy)", got)
	}
	if len(patches) != 1 || patches[0].Path != "/environments/env-b/services/svc-img/source" {
		t.Fatalf("PATCH = %+v, quero env-b (staging)", patches)
	}
}

// --image num serviço git o converteria em serviço de imagem sem ninguém pedir.
func TestSetServiceImageRefusesWithoutWriting(t *testing.T) {
	var patches []sourcePatch
	client := newImageAPI(t, &patches)

	cases := []struct {
		name, service, ref, wantErr string
	}{
		{"serviço git", "web", "nginx:1.27", "not from an image"},
		{"serviço inexistente", "nope", "nginx:1.27", "not found"},
		{"tag vazia de um lookup sem resultado", "mailpit", "axllent/mailpit:", "empty tag"},
		{"digest vazio", "mailpit", "axllent/mailpit@", "empty tag"},
		{"referência vazia", "mailpit", "", "invalid image reference"},
		{"espaço na referência", "mailpit", "nginx :1.27", "invalid image reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := setServiceImage(client, "proj-a", tc.service, tc.ref)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("erro = %v, quero conter %q", err, tc.wantErr)
			}
		})
	}
	if len(patches) != 0 {
		t.Fatalf("nenhum PATCH deveria sair, houve %+v", patches)
	}
}

func TestSetServiceImageNeedsAService(t *testing.T) {
	var patches []sourcePatch
	client := newImageAPI(t, &patches)
	cfg := linkedConfig()
	cfg.ServiceID = ""
	linkDirectory(t, cfg)

	_, err := setServiceImage(client, "proj-a", "", "nginx:1.27")
	if err == nil || !strings.Contains(err.Error(), "needs a service") {
		t.Fatalf("erro = %v, quero pedido de -s", err)
	}
}

func TestDeployImageRefusesWatch(t *testing.T) {
	var patches []sourcePatch
	newImageAPI(t, &patches)
	prevImage, prevWatch := deployImageFlag, deployWatchFlag
	t.Cleanup(func() { deployImageFlag, deployWatchFlag = prevImage, prevWatch })
	deployImageFlag, deployWatchFlag = "nginx:1.27", true

	err := deployCmd.RunE(deployCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--watch") {
		t.Fatalf("erro = %v, quero recusa de --image com --watch", err)
	}
	if len(patches) != 0 {
		t.Fatalf("nenhum PATCH deveria sair, houve %+v", patches)
	}
}
