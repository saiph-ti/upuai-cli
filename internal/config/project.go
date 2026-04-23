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
	ProjectID     string `json:"projectId"`
	ProjectName   string `json:"projectName"`
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

func SaveProjectConfig(cfg *ProjectConfig) error {
	dir := projectConfigDir
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("failed to create project config directory: %w", err)
	}

	addToGitignore()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal project config: %w", err)
	}

	path := filepath.Join(dir, projectConfigFile)
	if err := os.WriteFile(path, data, filePerm); err != nil {
		return fmt.Errorf("failed to write project config: %w", err)
	}
	return nil
}

func ProjectConfigExists() bool {
	return findProjectConfig(".") != ""
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
	defer f.Close()

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
