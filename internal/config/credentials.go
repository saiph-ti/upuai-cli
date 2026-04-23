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
	if err := os.WriteFile(s.path, data, filePerm); err != nil {
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

func (s *CredentialStore) GetToken() string {
	if token := os.Getenv("UPUAI_TOKEN"); token != "" {
		return token
	}
	creds, err := s.Load()
	if err != nil || creds == nil {
		return ""
	}
	return creds.Token
}
