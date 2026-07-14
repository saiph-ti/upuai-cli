package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// EnvTokenVar carries a scoped machine/CI token (minted by `upuai token create`)
// for non-interactive use. It takes precedence over the stored login credential.
const EnvTokenVar = "UPUAI_TOKEN"

// MachineTokenFromEnv returns the scoped machine token from UPUAI_TOKEN, or "".
func MachineTokenFromEnv() string {
	return strings.TrimSpace(os.Getenv(EnvTokenVar))
}

// GetToken returns the bearer credential to use. Precedence: a scoped machine
// token in UPUAI_TOKEN (non-interactive / CI) wins; otherwise the interactive
// login credential from `~/.upuai/credentials.json`, auto-rotated on 401 by the
// API client's refresh path.
//
// UPUAI_TOKEN previously (pre-2026-05-21) stuffed a short-lived user JWT here and
// was removed because the 2h TTL + refresh-token dependency broke headless CI. It
// now carries an OPAQUE, server-validated, long-lived scoped token that needs no
// refresh — the fix for exactly that breakage. The client's refresh path skips
// rotation whenever this env var is set. See runbook
// upuai-core/docs/runbooks/2026-07-14-scoped-api-tokens.md.
func (s *CredentialStore) GetToken() string {
	if envToken := MachineTokenFromEnv(); envToken != "" {
		return envToken
	}
	creds, err := s.Load()
	if err != nil || creds == nil {
		return ""
	}
	return creds.Token
}
