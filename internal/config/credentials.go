package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	upuaiDir        = ".upuai"
	credentialsFile = "credentials.json"
	dirPerm         = 0700
	filePerm        = 0600
)

type StoredUser struct {
	UserID   string `json:"userId"`
	UserName string `json:"userName"`
	Login    string `json:"login"`
}

type Credentials struct {
	Token        string     `json:"token"`
	RefreshToken string     `json:"refreshToken"`
	User         StoredUser `json:"user"`
	ApiURL       string     `json:"apiUrl"`
}

type CredentialStore struct {
	path string
}

func NewCredentialStore() *CredentialStore {
	home, _ := os.UserHomeDir()
	return &CredentialStore{
		path: filepath.Join(home, upuaiDir, credentialsFile),
	}
}

func (s *CredentialStore) Load() (*Credentials, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read credentials: %w", err)
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}
	return &creds, nil
}

func (s *CredentialStore) Save(creds *Credentials) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}
	// Escrita atômica: grava num temp 0600 no mesmo dir e renomeia por cima. O
	// rename preserva as perms do temp independente de um arquivo pré-existente
	// — os.WriteFile só aplicava 0600 na criação, então um credentials.json
	// legado com perms frouxas continuaria frouxo. Também evita escrita parcial.
	tmp, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp credentials file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op se o rename já consumiu o temp
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to set credentials permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to flush credentials: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}
	return nil
}

func (s *CredentialStore) Clear() error {
	err := os.Remove(s.path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove credentials: %w", err)
	}
	return nil
}

func (s *CredentialStore) Exists() bool {
	_, err := os.Stat(s.path)
	return err == nil
}

// GetToken returns the JWT to use as Bearer auth. Source of truth is the
// credential store (`~/.upuai/credentials.json`), populated by `upuai login`
// and auto-rotated on 401 by the API client's refresh path. There is no env
// var fallback — the previous `UPUAI_TOKEN` env shortcut was removed on
// 2026-05-21 because the JWT TTL (2h) plus the refresh requiring the
// refresh token from credentials.json made headless usage break silently
// in any CI job longer than the access token lifetime. See runbook
// upuai-core/docs/runbooks/2026-05-21-ai-deploy-skill.md (round 2).
func (s *CredentialStore) GetToken() string {
	creds, err := s.Load()
	if err != nil || creds == nil {
		return ""
	}
	return creds.Token
}
