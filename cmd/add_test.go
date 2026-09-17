package cmd

import "testing"

// O `--type` documentado é o valor da API (`docker_image`); o picker mostra a
// label ("docker image"). Aceitar só a label rejeitava o comando copiado da
// documentação.
func TestNormalizeServiceType(t *testing.T) {
	accepted := map[string]string{
		"docker image":    "docker_image",
		"docker_image":    "docker_image",
		"docker-image":    "docker_image",
		"DOCKER_IMAGE":    "docker_image",
		" docker  image ": "docker_image",
		"app":             "empty",
		"github":          "github",
		"bucket":          "bucket",
	}
	for raw, want := range accepted {
		got, ok := normalizeServiceType(raw)
		if !ok || got != want {
			t.Errorf("normalizeServiceType(%q) = %q, %v; want %q, true", raw, got, ok, want)
		}
	}

	for _, raw := range []string{"", "dockerimage", "docker images", "vm"} {
		if got, ok := normalizeServiceType(raw); ok {
			t.Errorf("normalizeServiceType(%q) = %q, true; want refusal", raw, got)
		}
	}
}
