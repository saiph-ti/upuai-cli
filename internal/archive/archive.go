// Package archive empacota o diretório de trabalho num tar.gz pra deploy de
// fonte local (`upuai up`), honrando ignores pra não subir lixo/segredo.
package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// alwaysIgnore são componentes de path sempre excluídos — independem de
// .gitignore. .git/.upuai (metadados), node_modules e build outputs (peso),
// e segredos (.env*) que nunca devem ir pro object storage (vars vêm da
// plataforma). Espelha o defaultIgnore do watcher + segredos.
var alwaysIgnore = map[string]bool{
	".git": true, ".upuai": true, "node_modules": true, ".next": true,
	"dist": true, "build": true, "__pycache__": true, ".venv": true,
	"vendor": true, "bin": true,
}

// Pack cria um tar.gz do diretório `dir` num arquivo temporário e devolve o
// caminho, o sha256 (hex) do tarball e o tamanho em bytes. O caller deve
// remover o arquivo (os.Remove). Ignora alwaysIgnore + .env* + padrões de
// .gitignore/.upuaiignore (subset pragmático: nome/dir/glob por componente).
func Pack(dir string) (path string, sha256hex string, size int64, err error) {
	patterns := loadIgnorePatterns(dir)

	tmp, err := os.CreateTemp("", "upuai-src-*.tar.gz")
	if err != nil {
		return "", "", 0, fmt.Errorf("create temp archive: %w", err)
	}
	tmpPath := tmp.Name()
	// Em qualquer erro depois daqui, removemos o temp pra não vazar.
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	// Escreve simultaneamente no arquivo e no hasher (sha do tarball final).
	gz := gzip.NewWriter(io.MultiWriter(tmp, hasher))
	tw := tar.NewWriter(gz)

	walkErr := filepath.Walk(dir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(dir, p)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isIgnored(rel, info.IsDir(), patterns) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Só arquivos regulares e diretórios; symlinks/sockets/devices ignorados
		// (build não precisa, e symlink fora da árvore é risco).
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		hdr, hErr := tar.FileInfoHeader(info, "")
		if hErr != nil {
			return hErr
		}
		hdr.Name = rel
		if wErr := tw.WriteHeader(hdr); wErr != nil {
			return wErr
		}
		f, oErr := os.Open(p)
		if oErr != nil {
			return oErr
		}
		defer func() { _ = f.Close() }()
		_, cErr := io.Copy(tw, f)
		return cErr
	})
	if walkErr != nil {
		_ = tw.Close()
		_ = gz.Close()
		_ = tmp.Close()
		err = fmt.Errorf("pack source: %w", walkErr)
		return "", "", 0, err
	}

	if err = tw.Close(); err != nil {
		_ = gz.Close()
		_ = tmp.Close()
		return "", "", 0, fmt.Errorf("close tar: %w", err)
	}
	if err = gz.Close(); err != nil {
		_ = tmp.Close()
		return "", "", 0, fmt.Errorf("close gzip: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return "", "", 0, fmt.Errorf("close archive: %w", err)
	}

	st, statErr := os.Stat(tmpPath)
	if statErr != nil {
		err = statErr
		return "", "", 0, fmt.Errorf("stat archive: %w", err)
	}
	return tmpPath, hex.EncodeToString(hasher.Sum(nil)), st.Size(), nil
}

// ExtractBinary devolve os bytes do executável `binaryName` de dentro de um arquivo
// `.tar.gz` ou `.zip` (detectado pelo sufixo de `assetName`). É usado pelo self-update
// (`upuai upgrade`) pra extrair o binário do release do GitHub — os assets do goreleaser
// são archives, não binários soltos, então gravar o asset cru por cima do executável
// (bug antigo) corrompia o CLI. Acha a entry cujo basename == binaryName.
func ExtractBinary(data []byte, assetName, binaryName string) ([]byte, error) {
	switch {
	case strings.HasSuffix(assetName, ".zip"):
		return extractFromZip(data, binaryName)
	case strings.HasSuffix(assetName, ".tar.gz"), strings.HasSuffix(assetName, ".tgz"):
		return extractFromTarGz(data, binaryName)
	default:
		return nil, fmt.Errorf("unsupported archive %q (want .tar.gz or .zip)", assetName)
	}
}

func extractFromTarGz(data []byte, binaryName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, nextErr := tr.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nil, fmt.Errorf("read tar: %w", nextErr)
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != binaryName {
			continue
		}
		b, readErr := io.ReadAll(tr)
		if readErr != nil {
			return nil, fmt.Errorf("read %q: %w", binaryName, readErr)
		}
		return b, nil
	}
	return nil, fmt.Errorf("binary %q not found in archive", binaryName)
}

func extractFromZip(data []byte, binaryName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || path.Base(f.Name) != binaryName {
			continue
		}
		rc, openErr := f.Open()
		if openErr != nil {
			return nil, fmt.Errorf("open %q: %w", binaryName, openErr)
		}
		b, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read %q: %w", binaryName, readErr)
		}
		return b, nil
	}
	return nil, fmt.Errorf("binary %q not found in archive", binaryName)
}

func loadIgnorePatterns(dir string) []string {
	var patterns []string
	for _, name := range []string{".gitignore", ".upuaiignore"} {
		data, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
				continue // negation (!) não suportada — subset pragmático.
			}
			patterns = append(patterns, strings.Trim(line, "/"))
		}
	}
	return patterns
}

// isIgnored decide se rel (path slash-separated relativo à raiz) é excluído.
func isIgnored(rel string, isDir bool, patterns []string) bool {
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	// Segredos nunca sobem.
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	// Componentes sempre ignorados (em qualquer nível).
	for _, comp := range strings.Split(rel, "/") {
		if alwaysIgnore[comp] {
			return true
		}
	}
	// Padrões de .gitignore/.upuaiignore (subset): match por path completo se o
	// padrão tem "/", senão por basename ou qualquer componente (glob simples).
	for _, pat := range patterns {
		if strings.Contains(pat, "/") {
			if ok, _ := filepath.Match(pat, rel); ok {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
		for _, comp := range strings.Split(rel, "/") {
			if ok, _ := filepath.Match(pat, comp); ok {
				return true
			}
		}
	}
	return false
}
