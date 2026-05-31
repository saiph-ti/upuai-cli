package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
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

func TestExtractBinaryTarGz(t *testing.T) {
	want := []byte("\x7fELF...fake binary bytes")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// Archive do goreleaser: binário na raiz junto de LICENSE/README.
	for _, e := range []struct{ name, body string }{{"LICENSE", "MIT"}, {"upuai", string(want)}} {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0o755, Size: int64(len(e.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()

	got, err := ExtractBinary(buf.Bytes(), "upuai_1.2.3_linux_x86_64.tar.gz", "upuai")
	if err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractBinaryZip(t *testing.T) {
	want := []byte("MZ...fake windows binary")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("upuai.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(want); err != nil {
		t.Fatal(err)
	}
	r, _ := zw.Create("README.md")
	_, _ = r.Write([]byte("hi"))
	_ = zw.Close()

	got, err := ExtractBinary(buf.Bytes(), "upuai_1.2.3_windows_x86_64.zip", "upuai.exe")
	if err != nil {
		t.Fatalf("ExtractBinary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractBinaryNotFound(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "LICENSE", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("MIT"))
	_ = tw.Close()
	_ = gz.Close()
	if _, err := ExtractBinary(buf.Bytes(), "x.tar.gz", "upuai"); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestExtractBinaryUnsupported(t *testing.T) {
	if _, err := ExtractBinary([]byte("x"), "upuai_1.2.3_linux_x86_64.rar", "upuai"); err == nil {
		t.Fatal("expected unsupported-archive error")
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
