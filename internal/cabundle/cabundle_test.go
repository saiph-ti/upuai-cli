package cabundle

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// expectedRoots trava o conteúdo do bundle embutido: subject e SHA-256 do DER
// de cada raiz, conferidos contra o CCADB (ver o comentário do pacote).
var expectedRoots = []struct {
	commonName   string
	organization string
	sha256       string
}{
	{"ISRG Root X1", "Internet Security Research Group", "96BCEC06264976F37460779ACF28C5A7CFE8A3C0AAE11A8FFCEE05C0BDDF08C6"},
	{"ISRG Root X2", "Internet Security Research Group", "69729B8E15A86EFC177A57AFB7171DFC64ADD28C2FCA8CF1507E34453CCB1470"},
	{"Root YE", "ISRG", "E14FFCAD5B0025731006CAA43A121A22D8E9700F4FB9CF852F02A708AA5D5666"},
	{"Root YR", "ISRG", "E57B7E6F150C419102E8D5C055729FF967B9D1A829BF00CEC89CA604EBF4A86F"},
}

func TestEmbeddedBundleHoldsTheFourISRGRoots(t *testing.T) {
	rest := EmbeddedPEM()
	var certs []*x509.Certificate
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			t.Fatalf("bloco PEM inesperado: %s", block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		certs = append(certs, cert)
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		t.Fatalf("sobra após o último certificado: %q", rest)
	}
	if len(certs) != len(expectedRoots) {
		t.Fatalf("bundle tem %d certificados, quero %d", len(certs), len(expectedRoots))
	}
	for i, want := range expectedRoots {
		c := certs[i]
		sum := sha256.Sum256(c.Raw)
		if got := strings.ToUpper(hex.EncodeToString(sum[:])); got != want.sha256 {
			t.Errorf("%s: SHA-256 = %s, quero %s", want.commonName, got, want.sha256)
		}
		if c.Subject.CommonName != want.commonName {
			t.Errorf("cert %d: CN = %q, quero %q", i, c.Subject.CommonName, want.commonName)
		}
		if len(c.Subject.Organization) != 1 || c.Subject.Organization[0] != want.organization {
			t.Errorf("%s: O = %v, quero %q", want.commonName, c.Subject.Organization, want.organization)
		}
		if !c.IsCA || !bytes.Equal(c.RawSubject, c.RawIssuer) {
			t.Errorf("%s: quero uma raiz autoassinada (CA)", want.commonName)
		}
		if err := c.CheckSignatureFrom(c); err != nil {
			t.Errorf("%s: autoassinatura inválida: %v", want.commonName, err)
		}
	}
}

// O comentário no topo de cada bloco documenta o fingerprint para quem lê o
// arquivo; ele precisa bater com o certificado logo abaixo.
func TestEmbeddedBundleCommentsMatchFingerprints(t *testing.T) {
	text := string(EmbeddedPEM())
	for _, want := range expectedRoots {
		var pairs []string
		for i := 0; i < len(want.sha256); i += 2 {
			pairs = append(pairs, want.sha256[i:i+2])
		}
		if !strings.Contains(text, "# SHA-256: "+strings.Join(pairs, ":")) {
			t.Errorf("comentário do fingerprint de %s ausente", want.commonName)
		}
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveOrder(t *testing.T) {
	dir := t.TempDir()
	envFile := writeFile(t, dir, "env.pem", "env")
	sysA := writeFile(t, dir, "sys-a.pem", "a")
	sysB := writeFile(t, dir, "sys-b.pem", "b")
	empty := writeFile(t, dir, "empty.pem", "")
	missing := filepath.Join(dir, "missing.pem")

	env := func(v string) func(string) string {
		return func(k string) string {
			if k == EnvCertFile {
				return v
			}
			return ""
		}
	}

	cases := []struct {
		name       string
		env        string
		candidates []string
		wantPath   string
		wantSource Source
	}{
		{"SSL_CERT_FILE vence o sistema", envFile, []string{sysA}, envFile, SourceEnv},
		{"SSL_CERT_FILE ilegível cai para o sistema", missing, []string{sysA}, sysA, SourceSystem},
		{"SSL_CERT_FILE vazio cai para o sistema", empty, []string{sysA}, sysA, SourceSystem},
		{"SSL_CERT_FILE diretório cai para o sistema", dir, []string{sysA}, sysA, SourceSystem},
		{"primeiro caminho conhecido existente", "", []string{missing, sysB, sysA}, sysB, SourceSystem},
		{"caminho conhecido vazio é pulado", "", []string{empty, sysA}, sysA, SourceSystem},
		{"nada no sistema usa o embutido", "", []string{missing}, "", SourceEmbedded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			got, err := resolver{getenv: env(tc.env), candidates: tc.candidates}.resolve(tmp)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got.Source != tc.wantSource {
				t.Fatalf("source = %s, quero %s", got.Source, tc.wantSource)
			}
			if tc.wantSource != SourceEmbedded {
				if got.Path != tc.wantPath {
					t.Fatalf("path = %s, quero %s", got.Path, tc.wantPath)
				}
				return
			}
			if filepath.Dir(got.Path) != tmp {
				t.Fatalf("bundle embutido fora do diretório do chamador: %s", got.Path)
			}
			data, err := os.ReadFile(got.Path)
			if err != nil || !bytes.Equal(data, embeddedRoots) {
				t.Fatalf("conteúdo gravado difere do embutido (err %v)", err)
			}
			if runtime.GOOS != "windows" {
				fi, _ := os.Stat(got.Path)
				if perm := fi.Mode().Perm(); perm != 0o600 {
					t.Fatalf("permissão = %o, quero 0600", perm)
				}
			}
		})
	}
}

// O bundle do sistema da máquina de teste, quando existe, é o que Resolve
// devolve sem SSL_CERT_FILE — trava a ligação Resolve → KnownPaths.
func TestResolveUsesKnownPaths(t *testing.T) {
	t.Setenv(EnvCertFile, "")
	got, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, p := range KnownPaths {
		if readableFile(p) {
			if got.Source != SourceSystem || got.Path != p {
				t.Fatalf("Resolve = %+v, quero %s do sistema", got, p)
			}
			return
		}
	}
	if got.Source != SourceEmbedded {
		t.Fatalf("Resolve = %+v, quero o embutido", got)
	}
}

func TestWriteEmbeddedRefusesExistingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteEmbedded(dir); err != nil {
		t.Fatalf("primeira gravação: %v", err)
	}
	// O_EXCL: nunca segue/reescreve um arquivo pré-existente no diretório.
	if _, err := WriteEmbedded(dir); err == nil {
		t.Fatal("quero erro ao regravar sobre um arquivo existente")
	}
	if _, err := WriteEmbedded(""); err == nil {
		t.Fatal("quero erro sem diretório")
	}
}
