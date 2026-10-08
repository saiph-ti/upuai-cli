// Package cabundle resolve o arquivo de CAs que os clientes de banco externos
// (mysql, mysqldump, mariadb) recebem em --ssl-ca para verificar a identidade
// do endpoint público (*.db.upuai.cloud, wildcard Let's Encrypt).
//
// O cliente MySQL não tem o equivalente ao `sslrootcert=system` da libpq: a
// verificação de identidade (VERIFY_IDENTITY) exige um arquivo de CAs
// explícito. A ordem de resolução é:
//
//  1. SSL_CERT_FILE, se definido e legível — a convenção do OpenSSL que o
//     usuário já usa para apontar outro trust store (proxy corporativo etc.);
//  2. o bundle do sistema operacional, nos caminhos conhecidos (KnownPaths);
//  3. o bundle embutido abaixo, gravado num arquivo 0600 dentro do diretório
//     temporário do chamador — o caminho do Windows, que não tem bundle PEM.
//
// # Bundle embutido
//
// letsencrypt-roots.pem traz as quatro raízes autoassinadas do ISRG, baixadas
// dos links "pem" de https://letsencrypt.org/certificates/ em 2026-10-07:
//
//	ISRG Root X1  (CN=ISRG Root X1, O=Internet Security Research Group) RSA 4096
//	  https://letsencrypt.org/certs/isrgrootx1.pem
//	  SHA-256 96:BC:EC:06:26:49:76:F3:74:60:77:9A:CF:28:C5:A7:CF:E8:A3:C0:AA:E1:1A:8F:FC:EE:05:C0:BD:DF:08:C6
//	ISRG Root X2  (CN=ISRG Root X2, O=Internet Security Research Group) ECDSA P-384
//	  https://letsencrypt.org/certs/isrg-root-x2.pem
//	  SHA-256 69:72:9B:8E:15:A8:6E:FC:17:7A:57:AF:B7:17:1D:FC:64:AD:D2:8C:2F:CA:8C:F1:50:7E:34:45:3C:CB:14:70
//	ISRG Root YE  (CN=Root YE, O=ISRG) ECDSA P-384
//	  https://letsencrypt.org/certs/gen-y/root-ye.pem
//	  SHA-256 E1:4F:FC:AD:5B:00:25:73:10:06:CA:A4:3A:12:1A:22:D8:E9:70:0F:4F:B9:CF:85:2F:02:A7:08:AA:5D:56:66
//	ISRG Root YR  (CN=Root YR, O=ISRG) RSA 4096
//	  https://letsencrypt.org/certs/gen-y/root-yr.pem
//	  SHA-256 E5:7B:7E:6F:15:0C:41:91:02:E8:D5:C0:55:72:9F:F9:67:B9:D1:A8:29:BF:00:CE:C8:9C:A6:04:EB:F4:A8:6F
//
// A página do Let's Encrypt não publica fingerprints (só os arquivos e links
// para o crt.sh). Cada fingerprint acima foi conferido contra o registro
// "Root Certificate" da raiz no CCADB (AllCertificateRecordsCSVFormatv4, a base
// comum dos programas de raiz Mozilla/Apple/Chrome/Microsoft, alimentada pela
// própria ISRG); X1 e X2 também contra o crt.sh linkado na página e o relatório
// de raízes incluídas da Mozilla. O PEM e o DER de cada raiz deram o mesmo hash.
//
// Por que as quatro: o wildcard servido hoje encadeia leaf ← YR2 ← Root YR, com
// Root YR assinada cruzada por ISRG Root X1; o Let's Encrypt pode passar a
// emitir pelo lado ECDSA (YE*, Root YE ← ISRG Root X2). Root YE/YR ainda não
// estão nos trust stores dos sistemas — embuti-las cobre a cadeia curta.
//
// Trocar este arquivo exige refazer a conferência acima e atualizar
// expectedRoots em cabundle_test.go.
package cabundle

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed letsencrypt-roots.pem
var embeddedRoots []byte

// EnvCertFile é a variável que, se definida e legível, vence qualquer outra fonte.
const EnvCertFile = "SSL_CERT_FILE"

// embeddedFileName é o nome do bundle embutido quando gravado em disco.
const embeddedFileName = "upuai-letsencrypt-roots.pem"

// KnownPaths são os bundles PEM dos sistemas operacionais, na ordem testada.
var KnownPaths = []string{
	"/etc/ssl/cert.pem",                  // macOS, Alpine, BSDs
	"/etc/ssl/certs/ca-certificates.crt", // Debian/Ubuntu, Arch, Gentoo
	"/etc/pki/tls/certs/ca-bundle.crt",   // RHEL/Fedora/CentOS
	"/etc/ssl/ca-bundle.pem",             // openSUSE
}

// Source diz de onde veio o bundle resolvido.
type Source string

const (
	SourceEnv      Source = "env"      // SSL_CERT_FILE
	SourceSystem   Source = "system"   // um dos KnownPaths
	SourceEmbedded Source = "embedded" // raízes Let's Encrypt embutidas
)

// Bundle é o resultado da resolução.
type Bundle struct {
	Path   string
	Source Source
}

// EmbeddedPEM devolve uma cópia do bundle embutido.
func EmbeddedPEM() []byte {
	out := make([]byte, len(embeddedRoots))
	copy(out, embeddedRoots)
	return out
}

// Resolve escolhe o bundle de CAs. tempDir é um diretório privado do chamador
// (criado com os.MkdirTemp, 0700) onde o bundle embutido é gravado quando
// nenhuma outra fonte serve; o chamador o remove ao terminar.
func Resolve(tempDir string) (Bundle, error) {
	return resolver{getenv: os.Getenv, candidates: KnownPaths}.resolve(tempDir)
}

// resolver isola o ambiente e os caminhos para teste.
type resolver struct {
	getenv     func(string) string
	candidates []string
}

func (r resolver) resolve(tempDir string) (Bundle, error) {
	if p := r.getenv(EnvCertFile); p != "" && readableFile(p) {
		return Bundle{Path: p, Source: SourceEnv}, nil
	}
	for _, p := range r.candidates {
		if readableFile(p) {
			return Bundle{Path: p, Source: SourceSystem}, nil
		}
	}
	path, err := WriteEmbedded(tempDir)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{Path: path, Source: SourceEmbedded}, nil
}

// WriteEmbedded grava o bundle embutido em dir com permissão 0600.
func WriteEmbedded(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("write CA bundle: no directory")
	}
	path := filepath.Join(dir, embeddedFileName)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("write CA bundle: %w", err)
	}
	if _, err := f.Write(embeddedRoots); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write CA bundle: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("write CA bundle: %w", err)
	}
	return path, nil
}

// readableFile: arquivo regular, não vazio, que este processo consegue abrir.
// Um SSL_CERT_FILE apontando para um caminho velho não pode virar um --ssl-ca
// que o cliente recusa com erro de TLS opaco — cai para a próxima fonte.
func readableFile(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
