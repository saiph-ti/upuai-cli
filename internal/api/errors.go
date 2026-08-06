package api

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type APIError struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
	Path       string `json:"path,omitempty"`
	// Code é o identificador ESTÁVEL do erro no catálogo da API
	// (apps/api/src/lib/error-codes.ts) — ex: NOT_A_MEMBER, PLAN_LIMIT_DOMAINS.
	// É o único campo do payload seguro para ramificar: `message` é texto voltado
	// ao humano, traduzível e reescrito sem aviso, e `error` é o classname legado.
	// Comparar mensagem seria acoplar o CLI a uma string de UI.
	Code string `json:"code,omitempty"`
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

// ErrorCode extrai o código estável do catálogo de um erro devolvido pelo
// client, ou "" se o erro não veio da API (rede, parse) ou se a resposta não
// trouxe `code`. Usa errors.As para atravessar wraps de fmt.Errorf("%w") — os
// call-sites embrulham com contexto antes de propagar.
func ErrorCode(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

// StatusCode extrai o HTTP status de um erro da API, ou 0 se o erro não veio
// da API. Mesma motivação do ErrorCode: evita type assertion repetida e
// funciona através de wraps.
func StatusCode(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
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
