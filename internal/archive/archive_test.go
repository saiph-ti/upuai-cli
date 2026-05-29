package archive

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestPackHonorsIgnores(t *testing.T) {
	dir := t.TempDir()

	// Layout: arquivos que devem entrar e que devem ser excluídos.
	mustWrite(t, dir, "main.go", "package main")
	mustWrite(t, dir, "upuai.toml", "[build]")
	mustWrite(t, dir, ".env", "SECRET=1")               // segredo → excluído sempre
	mustWrite(t, dir, ".env.local", "SECRET=2")         // segredo → excluído sempre
	mustWrite(t, dir, "node_modules/dep/index.js", "x") // alwaysIgnore
	mustWrite(t, dir, ".git/config", "[core]")          // alwaysIgnore
	mustWrite(t, dir, "build.log", "noise")             // via .gitignore
	mustWrite(t, dir, "src/app.go", "package src")
	mustWrite(t, dir, ".gitignore", "*.log\n")

	tarPath, sum, size, err := Pack(dir)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	defer func() { _ = os.Remove(tarPath) }()

	if sum == "" || size == 0 {
		t.Fatalf("expected non-empty sha256 and size, got sum=%q size=%d", sum, size)
	}

	got := tarEntries(t, tarPath)
	sort.Strings(got)
	want := []string{".gitignore", "main.go", "src/app.go", "upuai.toml"}

	if len(got) != len(want) {
		t.Fatalf("tar entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tar entries = %v, want %v", got, want)
		}
	}
}

func mustWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tarEntries(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		names = append(names, hdr.Name)
	}
	return names
}
