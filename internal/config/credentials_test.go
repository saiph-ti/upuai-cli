package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Regressão #9: Save escrevia com os.WriteFile(...,0600), que só aplica perms na
// CRIAÇÃO. Um credentials.json pré-existente com perms frouxas (ex: 0644 de um
// build antigo) continuava frouxo. Agora a escrita é atômica (temp 0600 +
// rename), forçando 0600 mesmo sobre um arquivo já existente.
func TestSaveEnforces0600OverLooseExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".upuai", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	// Arquivo pré-existente com perms frouxas.
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	s := &CredentialStore{path: path}
	if err := s.Save(&Credentials{Token: "t", RefreshToken: "r", ApiURL: "http://x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Fatalf("esperado 0600 após Save sobre arquivo 0644, veio %o", perm)
	}

	// Round-trip do conteúdo.
	got, err := s.Load()
	if err != nil || got == nil || got.Token != "t" || got.RefreshToken != "r" {
		t.Fatalf("Load após Save: %+v err=%v", got, err)
	}
}

// Sanidade: criação nova também fica 0600.
func TestSaveCreatesAt0600(t *testing.T) {
	dir := t.TempDir()
	s := &CredentialStore{path: filepath.Join(dir, ".upuai", "credentials.json")}
	if err := s.Save(&Credentials{Token: "t"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Fatalf("esperado 0600 na criação, veio %o", perm)
	}
}
