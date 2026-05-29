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

// hostProvider mapeia o host git pro provider canônico que a API entende. A
// allowlist é explícita pra (a) não confundir owner names com "." (ex:
// my.org/repo é canônico) e (b) detectar o provider a partir da URL em vez de
// assumir github. github/gitlab são suportados pela plataforma; os demais são
// reconhecidos só pra dar erro claro (não fallback silencioso pra github).
var hostProvider = map[string]string{
	"github.com":     "github",
	"www.github.com": "github",
	"gitlab.com":     "gitlab",
	"bitbucket.org":  "bitbucket",
	"codeberg.org":   "codeberg",
}

// ParseRepo aceita variantes comuns e retorna sempre "owner/repo". Mantido pra
// callers que não precisam do provider. Ver ParseRepoWithProvider.
func ParseRepo(input string) (string, error) {
	repo, _, err := ParseRepoWithProvider(input)
	return repo, err
}

// ParseRepoWithProvider normaliza o input pra "owner/repo" E devolve o provider
// derivado do host:
//
//	https://github.com/o/r       → o/r, "github"
//	https://gitlab.com/o/r.git   → o/r, "gitlab"
//	git@gitlab.com:o/r.git       → o/r, "gitlab"
//	gitlab.com/o/r               → o/r, "gitlab"
//	o/r                          → o/r, ""        (shorthand, sem host → provider desconhecido)
//	https://git.acme.com/o/r     → o/r, "git.acme.com"  (host explícito não-suportado)
//
// provider == "" significa "shorthand sem host" — o caller decide o default.
// provider != "" e fora de {github,gitlab} é um host explícito não-suportado —
// o caller deve rejeitar em vez de tratar como github.
//
// Rejeita: vazio, com auth embedded ("user:tok@host"), > 2 segments, chars inválidos.
func ParseRepoWithProvider(input string) (repo string, provider string, err error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", "", fmt.Errorf("repo is empty")
	}

	host := ""
	switch {
	case strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://"):
		stripped := strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
		hostAndPath, rest, _ := strings.Cut(stripped, "/")
		// Auth embedded em URL HTTP(S) — recusar pra não vazar credencial pro DB.
		if strings.Contains(hostAndPath, "@") {
			return "", "", fmt.Errorf("repo URL contains embedded credentials; remove user:token@ before passing --repo")
		}
		host = hostAndPath
		s = rest
	case strings.HasPrefix(s, "git@"):
		// git@host:owner/repo(.git)
		hostPart, after, ok := strings.Cut(strings.TrimPrefix(s, "git@"), ":")
		if !ok {
			return "", "", fmt.Errorf("invalid SSH repo URL %q (expected git@host:owner/repo)", input)
		}
		host = hostPart
		s = after
	default:
		// "host/owner/repo" passado direto (sem scheme) — só strippa se for host conhecido.
		for h := range hostProvider {
			if strings.HasPrefix(s, h+"/") {
				host = h
				s = strings.TrimPrefix(s, h+"/")
				break
			}
		}
	}

	provider = hostProvider[host]
	if provider == "" && host != "" {
		// Host explícito mas não mapeado — devolve o host cru pro caller rejeitar.
		provider = host
	}

	// strip trailing .git e slash
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimSuffix(s, "/")

	if !repoFullNameRegex.MatchString(s) {
		return "", "", fmt.Errorf("invalid repo %q: expected 'owner/repo' format (e.g. gbmiranda/geniapost)", input)
	}
	return s, provider, nil
}

// ResolveProvider decide o provider git de um fluxo --repo. `explicit` é o
// provider vindo de --type ("" se não passado, senão "github"/"gitlab" já
// mapeado); `detected` é o que ParseRepoWithProvider derivou do host da URL
// ("" pra shorthand owner/repo sem host).
//
//   - --type explícito vence (mas erra se conflitar com um host suportado);
//   - senão usa o provider detectado, se suportado;
//   - shorthand sem host cai no default github (paridade com o comportamento legado);
//   - host explícito não-suportado → erro claro (nunca fallback silencioso pra github).
func ResolveProvider(explicit, detected string) (string, error) {
	supported := detected == "github" || detected == "gitlab"
	switch {
	case explicit != "":
		if supported && detected != explicit {
			return "", fmt.Errorf("--type %s conflita com o host da URL (%s); remova um dos dois", explicit, detected)
		}
		return explicit, nil
	case supported:
		return detected, nil
	case detected == "":
		return "github", nil
	default:
		return "", fmt.Errorf("provider %q não suportado; use um repo do github.com ou gitlab.com (ou conecte via dashboard)", detected)
	}
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
