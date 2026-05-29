// Package archive empacota o diretório de trabalho num tar.gz pra deploy de
// fonte local (`upuai up`), honrando ignores pra não subir lixo/segredo.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
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
