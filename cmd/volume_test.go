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

func TestMatchVolumeRef(t *testing.T) {
	volumes := []api.Volume{
		{ID: "vol_1", Name: "data"},
		{ID: "vol_2", Name: "uploads"},
	}
	for ref, want := range map[string]string{
		"vol_2":   "vol_2",
		"data":    "vol_1",
		"UPLOADS": "vol_2",
	} {
		got, err := matchVolumeRef(volumes, ref)
		if err != nil || got.ID != want {
			t.Errorf("matchVolumeRef(%q) = (%v, %v); quero %s", ref, got.ID, err, want)
		}
	}
	if _, err := matchVolumeRef(volumes, "nope"); err == nil {
		t.Error("quero erro para volume inexistente")
	}
}

func TestFormatVolumeSize(t *testing.T) {
	for in, want := range map[int]string{0: "—", 1024: "1 GB", 5120: "5 GB", 1536: "1536 MB"} {
		if got := formatVolumeSize(in); got != want {
			t.Errorf("formatVolumeSize(%d) = %q; quero %q", in, got, want)
		}
	}
}

// O POST tem que sair com o serviço e o ambiente RESOLVIDOS e o tamanho em MB —
// um GB solto no corpo criaria um disco 1024× menor que o pedido.
func TestCreateVolumeWire(t *testing.T) {
	var got api.CreateVolumeRequest
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/proj-a/volumes", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"vol_9","name":"data","sizeMb":5120,"projectId":"proj-a"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPUAI_API_URL", srv.URL)
	t.Setenv(config.EnvTokenVar, "upua_ci_token")

	client := api.NewClient()
	volume, err := client.CreateVolume("proj-a", api.CreateVolumeRequest{
		MountPath:     "/data",
		ServiceID:     "svc-1",
		EnvironmentID: "env-a",
		SizeMb:        5 * mbPerGb,
	})
	if err != nil {
		t.Fatalf("CreateVolume: %v", err)
	}
	if got.SizeMb != 5120 || got.MountPath != "/data" || got.ServiceID != "svc-1" || got.EnvironmentID != "env-a" {
		t.Fatalf("corpo enviado = %+v", got)
	}
	if volume.ID != "vol_9" || !strings.EqualFold(volume.Name, "data") {
		t.Fatalf("resposta = %+v", volume)
	}
}
