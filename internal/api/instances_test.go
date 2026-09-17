package api

import (
	"encoding/json"
	"testing"
)

// config show -o json é como um pipeline lê a imagem atual antes de trocá-la.
func TestInstanceDecodesSource(t *testing.T) {
	raw := `{"id":"si-1","config":{"source":{"image":"axllent/mailpit:v1.24.0"},"deploy":{"startCommand":"x"}}}`
	var inst Instance
	if err := json.Unmarshal([]byte(raw), &inst); err != nil {
		t.Fatal(err)
	}
	if inst.Config == nil || inst.Config.Source == nil || inst.Config.Source.Image != "axllent/mailpit:v1.24.0" {
		t.Fatalf("imagem não decodificada: %+v", inst.Config)
	}
	out, err := json.Marshal(inst)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	_ = json.Unmarshal(out, &back)
	source := back["config"].(map[string]any)["source"].(map[string]any)
	if source["image"] != "axllent/mailpit:v1.24.0" {
		t.Fatalf("config show -o json perderia a imagem: %s", out)
	}
}

// O corpo do PATCH …/instance não pode carregar imagem nem repositório — a API
// recusa e manda usar …/source.
func TestUpdateInstanceRequestHasNoSourceIdentity(t *testing.T) {
	body, _ := json.Marshal(UpdateInstanceRequest{Source: &InstanceSourceConfig{RootDirectory: "apps/api"}})
	if string(body) != `{"source":{"rootDirectory":"apps/api"}}` {
		t.Fatalf("corpo inesperado: %s", body)
	}
}
