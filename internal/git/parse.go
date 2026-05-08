// Package git normaliza referências a repositórios git pra o formato canônico
// "owner/repo" usado pela API. Aceita URLs comuns e devolve erro com mensagem
// útil pro usuário em vez de deixar o backend rejeitar com 400 depois do
// roundtrip.
package git

import (
	"fmt"
	"regexp"
	"strings"
)

// repoFullNameRegex espelha a versão server-side em
// upuai-web/apps/api/src/schemas/service-instance-schema.ts.
var repoFullNameRegex = regexp.MustCompile(`^[\w.-]+/[\w.-]+$`)

// knownHosts são prefixos que ParseRepo strippa do input. Allowlist explícita
// pra não confundir com owner names contendo "." (ex: my.org/repo é canônico).
var knownHosts = []string{
	"github.com/",
	"www.github.com/",
	"gitlab.com/",
	"bitbucket.org/",
	"codeberg.org/",
}

// ParseRepo aceita variantes comuns e retorna sempre "owner/repo":
//
//	https://github.com/o/r       → o/r
//	https://github.com/o/r.git   → o/r
//	git@github.com:o/r.git       → o/r
//	github.com/o/r               → o/r
//	o/r                          → o/r
//
// Rejeita: vazio, com auth embedded ("user:tok@host"), > 2 segments, chars inválidos.
func ParseRepo(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", fmt.Errorf("repo is empty")
	}

	// Auth embedded em URL HTTP(S) — recusar pra não vazar credencial pro DB.
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		stripped := strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
		hostAndPath, _, _ := strings.Cut(stripped, "/")
		if strings.Contains(hostAndPath, "@") {
			return "", fmt.Errorf("repo URL contains embedded credentials; remove user:token@ before passing --repo")
		}
		s = stripped
	} else if strings.HasPrefix(s, "git@") {
		// git@host:owner/repo(.git)
		_, after, ok := strings.Cut(s, ":")
		if !ok {
			return "", fmt.Errorf("invalid SSH repo URL %q (expected git@host:owner/repo)", input)
		}
		s = after
	}

	// strip known git host prefix se ainda estiver lá (ex: "github.com/o/r"
	// passado direto, ou sobrou após strip de https://).
	for _, h := range knownHosts {
		if strings.HasPrefix(s, h) {
			s = strings.TrimPrefix(s, h)
			break
		}
	}

	// strip trailing .git
	s = strings.TrimSuffix(s, ".git")
	// strip trailing slash
	s = strings.TrimSuffix(s, "/")

	if !repoFullNameRegex.MatchString(s) {
		return "", fmt.Errorf("invalid repo %q: expected 'owner/repo' format (e.g. gbmiranda/geniapost)", input)
	}
	return s, nil
}

// NormalizeRootDir reduz expressões equivalentes à raiz pra "" — formato que a
// UI grava. Também tira "./" no começo pra paridade com o file picker.
func NormalizeRootDir(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "." || s == "/" || s == "./" {
		return ""
	}
	s = strings.TrimPrefix(s, "./")
	return s
}
