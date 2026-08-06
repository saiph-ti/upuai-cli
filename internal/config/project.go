package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const projectConfigDir = ".upuai"
const projectConfigFile = "config.json"

type ProjectConfig struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	// WorkspaceID fixa o diretório ao workspace dono do projeto. Um usuário
	// membro de vários workspaces tem uma sessão escopada a UM deles por vez
	// (o claim tenantId do JWT), e um projeto de outro workspace responde 404
	// indistinguível de "não existe" — o filtro de acesso da API é fail-closed
	// por design. Com o pin, o CLI detecta a divergência e troca a sessão antes
	// de chamar a API, em vez de deixar o usuário diante de um 404 mudo.
	//
	// omitempty + tolerância a vazio: configs gravados antes deste campo
	// existirem continuam válidos e são curados sob demanda via
	// GET /tenant/resolve. Nunca trate ausência como erro.
	WorkspaceID   string `json:"workspaceId,omitempty"`
	WorkspaceName string `json:"workspaceName,omitempty"`
	ServiceID     string `json:"serviceId,omitempty"`
	ServiceName   string `json:"serviceName,omitempty"`
	EnvironmentID string `json:"environmentId,omitempty"`
	Environment   string `json:"environment"`
	Framework     string `json:"framework,omitempty"`
}

func LoadProjectConfig() (*ProjectConfig, error) {
	path := findProjectConfig(".")
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read project config: %w", err)
	}
	var cfg ProjectConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse project config: %w", err)
	}
	return &cfg, nil
}

// SaveProjectConfig declares the CWD as the project root: it creates
// `./.upuai/config.json`. This is the `init`/`link` semantic — "this directory
// IS the project" — and is the only place a project config is born.
//
// To CHANGE an already-linked project, use UpdateProjectConfig instead: this
// function always writes relative to the CWD, so calling it from a subdirectory
// of a linked project would create a second, shadowing config there.
func SaveProjectConfig(cfg *ProjectConfig) error {
	dir := projectConfigDir
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("failed to create project config directory: %w", err)
	}

	addToGitignore()

	return writeProjectConfig(filepath.Join(dir, projectConfigFile), cfg)
}

// UpdateProjectConfig applies mutate to the project config that owns the CWD and
// writes it back to THE SAME file, wherever it lives up the tree.
//
// Every in-place change (env switch, service (un)link, workspace pin) must go
// through here. The old load-mutate-SaveProjectConfig pattern silently wrote to
// the CWD: run from a subdirectory, it left a brand-new config in the subdir that
// shadowed the real one at the root — the root kept the stale value and the two
// drifted apart with no error, since LoadProjectConfig walks up and finds the
// innermost one first.
//
// Returns errNoProjectConfig when the CWD is not inside a linked project.
func UpdateProjectConfig(mutate func(*ProjectConfig)) error {
	path := findProjectConfig(".")
	if path == "" {
		return errNoProjectConfig
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read project config: %w", err)
	}
	var cfg ProjectConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse project config: %w", err)
	}
	mutate(&cfg)
	return writeProjectConfig(path, &cfg)
}

// errNoProjectConfig sinaliza "nenhum projeto linkado" para UpdateProjectConfig.
// Os call-sites que atualizam de forma best-effort (backfill do workspace) o
// ignoram; os que exigem projeto já falharam antes, em requireProject.
var errNoProjectConfig = fmt.Errorf("no project config found")

// writeProjectConfig grava o config de forma atômica (temp no mesmo diretório +
// rename). Sem isso, um crash ou disco cheio no meio do os.WriteFile deixava um
// config.json truncado — e um config ilegível derruba TODO comando de projeto,
// exigindo `upuai link` de novo pra sair do buraco.
func writeProjectConfig(path string, cfg *ProjectConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal project config: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp project config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op se o rename já consumiu o temp

	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to set project config permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write project config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to flush project config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to write project config: %w", err)
	}
	return nil
}

func ProjectConfigExists() bool {
	return findProjectConfig(".") != ""
}

// ProjectRoot returns the directory that owns the linked project — the one
// containing `.upuai/config.json`, found by walking up from the CWD. ok is false
// when the CWD is not inside a linked project. Callers that write project-level
// files (e.g. the Claude Code skill) must anchor to this, not the CWD, so running
// a command from a subdirectory doesn't scatter files in the wrong place.
func ProjectRoot() (string, bool) {
	cfgPath := findProjectConfig(".")
	if cfgPath == "" {
		return "", false
	}
	// cfgPath = <root>/.upuai/config.json → root = dir(dir(cfgPath))
	return filepath.Dir(filepath.Dir(cfgPath)), true
}

func findProjectConfig(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		path := filepath.Join(abs, projectConfigDir, projectConfigFile)
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	return ""
}

func addToGitignore() {
	gitignorePath := ".gitignore"
	entry := ".upuai/"

	data, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return
	}

	content := string(data)
	for _, line := range splitLines(content) {
		if line == entry || line == ".upuai" {
			return
		}
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	if len(data) > 0 && data[len(data)-1] != '\n' {
		_, _ = f.WriteString("\n")
	}
	_, _ = f.WriteString(entry + "\n")
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
