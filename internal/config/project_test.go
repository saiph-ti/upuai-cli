package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// chdir troca o CWD do teste e restaura no cleanup. LoadProjectConfig /
// UpdateProjectConfig resolvem o config subindo a árvore a partir do CWD, então
// os testes precisam simular "rodar o comando de um subdiretório".
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func writeConfigAt(t *testing.T, root string, cfg *ProjectConfig) string {
	t.Helper()
	dir := filepath.Join(root, projectConfigDir)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, projectConfigFile)
	if err := os.WriteFile(path, data, filePerm); err != nil {
		t.Fatal(err)
	}
	return path
}

// UpdateProjectConfig deve gravar no config que JÁ EXISTE subindo a árvore, não
// no CWD. O padrão antigo (load-mutate-SaveProjectConfig) rodado de um
// subdiretório criava um segundo config lá dentro: LoadProjectConfig passava a
// achar o de dentro primeiro, o do root ficava com o valor velho, e os dois
// divergiam em silêncio. Atingia `env switch`, `add` e `service delete`.
func TestUpdateProjectConfigWritesToRootNotCwd(t *testing.T) {
	root := t.TempDir()
	rootConfig := writeConfigAt(t, root, &ProjectConfig{
		ProjectID:   "proj-1",
		ProjectName: "app",
		Environment: "staging",
	})

	sub := filepath.Join(root, "apps", "api")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	if err := UpdateProjectConfig(func(c *ProjectConfig) { c.Environment = "production" }); err != nil {
		t.Fatalf("UpdateProjectConfig: %v", err)
	}

	if _, err := os.Stat(filepath.Join(sub, projectConfigDir, projectConfigFile)); !os.IsNotExist(err) {
		t.Fatal("config sombra criado no subdiretório — a atualização deve ir para o config do root")
	}

	data, err := os.ReadFile(rootConfig)
	if err != nil {
		t.Fatal(err)
	}
	var got ProjectConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Environment != "production" {
		t.Fatalf("config do root não foi atualizado: %+v", got)
	}
	if got.ProjectID != "proj-1" || got.ProjectName != "app" {
		t.Fatalf("campos não mutados foram perdidos: %+v", got)
	}
}

func TestUpdateProjectConfigWithoutLinkedProject(t *testing.T) {
	chdir(t, t.TempDir())
	if err := UpdateProjectConfig(func(*ProjectConfig) {}); err == nil {
		t.Fatal("esperado erro quando não há projeto linkado")
	}
}

// Configs gravados antes do pin de workspace existir precisam continuar
// carregando — ausência do campo é estado válido, curado sob demanda, nunca erro.
func TestProjectConfigBackwardCompatibleWithoutWorkspace(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, projectConfigDir)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		t.Fatal(err)
	}
	legacy := `{"projectId":"p1","projectName":"legacy","environment":"staging","serviceId":"s1"}`
	if err := os.WriteFile(filepath.Join(dir, projectConfigFile), []byte(legacy), filePerm); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	cfg, err := LoadProjectConfig()
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	if cfg == nil || cfg.ProjectID != "p1" || cfg.ServiceID != "s1" {
		t.Fatalf("config legado não carregou: %+v", cfg)
	}
	if cfg.WorkspaceID != "" {
		t.Fatalf("esperado workspace vazio no config legado, veio %q", cfg.WorkspaceID)
	}

	// E o backfill grava o pin sem destruir o resto.
	err = UpdateProjectConfig(func(c *ProjectConfig) {
		c.WorkspaceID = "ws-1"
		c.WorkspaceName = "TAI"
	})
	if err != nil {
		t.Fatalf("UpdateProjectConfig: %v", err)
	}

	cfg, err = LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WorkspaceID != "ws-1" || cfg.WorkspaceName != "TAI" {
		t.Fatalf("pin não gravado: %+v", cfg)
	}
	if cfg.ServiceID != "s1" || cfg.Environment != "staging" {
		t.Fatalf("campos legados perdidos no backfill: %+v", cfg)
	}
}

// Um config sem workspace não pode serializar `"workspaceId": ""` — o campo é
// omitempty justamente para que "nunca pinado" e "pinado em vazio" não virem
// estados distintos no arquivo.
func TestProjectConfigOmitsEmptyWorkspace(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)

	err := SaveProjectConfig(&ProjectConfig{
		ProjectID:   "p1",
		ProjectName: "app",
		Environment: "staging",
	})
	if err != nil {
		t.Fatalf("SaveProjectConfig: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, projectConfigDir, projectConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["workspaceId"]; ok {
		t.Fatalf("workspaceId vazio não deveria ser serializado: %s", data)
	}
}
