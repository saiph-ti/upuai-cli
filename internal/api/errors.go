package api

import (
	"fmt"
	"sort"
	"strings"
)

type APIError struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
	Path       string `json:"path,omitempty"`
	// Details são os erros field-level que a API envia em falha de validação
	// (ex: {"hostname": "Hostname is required"}). Sem isso, o usuário só via
	// "Invalid data" — o padrão opaco que escondia a causa real (campo errado,
	// formato inválido, etc.). Surfacing torna o 400 auto-diagnosticável.
	Details map[string]string `json:"details,omitempty"`
	// RequestID vem do Fastify error-handler quando disponível. Em prod a API
	// mascara `message` ("Internal server error") e o RequestID é a única
	// âncora que o usuário tem pra abrir suporte sem ler stack trace.
	RequestID string `json:"requestId,omitempty"`
}

// detailsString renderiza os erros field-level de forma determinística
// (ordenada por campo) — ex: "hostname: Hostname is required".
func (e *APIError) detailsString() string {
	if len(e.Details) == 0 {
		return ""
	}
	keys := make([]string, 0, len(e.Details))
	for k := range e.Details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			parts = append(parts, e.Details[k])
		} else {
			parts = append(parts, fmt.Sprintf("%s: %s", k, e.Details[k]))
		}
	}
	return strings.Join(parts, "; ")
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
	if d := e.detailsString(); d != "" {
		msg += fmt.Sprintf(" (%s)", d)
	}
	if e.RequestID != "" {
		msg += fmt.Sprintf(" (requestId: %s)", e.RequestID)
	}
	return msg
}
