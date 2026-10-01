package cmd

import (
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
)

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

// O help de `upuai add --engine` anuncia postgres/mongo, mas o catálogo de
// templates usa postgresql/mongodb. O nome curto tem que casar (antes era
// recusado com "no managed database template matches engine").
func TestPickDatabaseTemplateAcceptsEngineAliases(t *testing.T) {
	templates := []api.DatabaseTemplate{
		{ID: "tpl-pg", Name: "PostgreSQL", Engine: "postgresql", Version: "18"},
		{ID: "tpl-mysql", Name: "MySQL", Engine: "mysql", Version: "8.4"},
		{ID: "tpl-redis", Name: "Redis", Engine: "redis", Version: "7.4"},
		{ID: "tpl-mongo", Name: "MongoDB", Engine: "mongodb", Version: "7.0"},
	}
	for engine, want := range map[string]string{
		"postgres":   "tpl-pg",
		"Postgres":   "tpl-pg",
		" postgres ": "tpl-pg",
		"postgresql": "tpl-pg",
		"PostgreSQL": "tpl-pg",
		"mongo":      "tpl-mongo",
		"mongodb":    "tpl-mongo",
		"MongoDB":    "tpl-mongo",
		"mysql":      "tpl-mysql",
		"redis":      "tpl-redis",
	} {
		got, err := pickDatabaseTemplate(templates, engine)
		if err != nil || got.ID != want {
			t.Errorf("pickDatabaseTemplate(%q) = %v, %v; want %s", engine, got, err, want)
		}
	}
	if _, err := pickDatabaseTemplate(templates, "oracle"); err == nil {
		t.Error("engine desconhecido deveria ser recusado")
	}
}
