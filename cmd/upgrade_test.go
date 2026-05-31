package cmd

import "testing"

func TestAssetSuffix(t *testing.T) {
	// Bate com os assets reais do release (upuai_<ver>_<os>_<arch>.<ext>).
	tests := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "_linux_x86_64.tar.gz"},
		{"linux", "arm64", "_linux_arm64.tar.gz"},
		{"darwin", "amd64", "_darwin_x86_64.tar.gz"},
		{"darwin", "arm64", "_darwin_arm64.tar.gz"},
		{"windows", "amd64", "_windows_x86_64.zip"},
	}
	for _, tc := range tests {
		if got := assetSuffix(tc.goos, tc.goarch); got != tc.want {
			t.Errorf("assetSuffix(%q, %q) = %q, want %q", tc.goos, tc.goarch, got, tc.want)
		}
	}
}

func TestAssetSuffixMatchesRealAssetNames(t *testing.T) {
	// Sufixo precisa casar o nome real (com versão no meio) e NÃO casar checksums.txt.
	real := "upuai_0.12.3_linux_x86_64.tar.gz"
	suffix := assetSuffix("linux", "amd64")
	if !hasSuffix(real, suffix) {
		t.Fatalf("%q should end with %q", real, suffix)
	}
	if hasSuffix("checksums.txt", suffix) {
		t.Fatal("checksums.txt must not match the asset suffix")
	}
}

func TestBinaryName(t *testing.T) {
	if got := binaryName("windows"); got != "upuai.exe" {
		t.Errorf("binaryName(windows) = %q, want upuai.exe", got)
	}
	for _, goos := range []string{"linux", "darwin"} {
		if got := binaryName(goos); got != "upuai" {
			t.Errorf("binaryName(%q) = %q, want upuai", goos, got)
		}
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
