package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func respWith(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestAPIError_SurfacesValidationDetails(t *testing.T) {
	c := &Client{}
	// Validation error: details is Record<string,string> (Zod parse). Should be surfaced.
	err := c.parseError(respWith(400, `{"statusCode":400,"error":"ValidationError","message":"Invalid data","details":{"hostname":"Hostname is required"},"requestId":"req-6j8"}`))
	got := err.Error()
	for _, want := range []string{"400", "Invalid data", "hostname: Hostname is required", "req-6j8"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestAPIError_PlanLimitNumericDetails_NoRegression(t *testing.T) {
	c := &Client{}
	// PlanLimit: details has non-string (numeric) values. Must NOT lose message.
	err := c.parseError(respWith(403, `{"statusCode":403,"code":"PLAN_LIMIT_DOMAINS","message":"Domain limit reached","details":{"current":3,"limit":3,"plan":"FREE"},"requestId":"req-x"}`))
	got := err.Error()
	if !strings.Contains(got, "Domain limit reached") || !strings.Contains(got, "req-x") {
		t.Fatalf("message/requestId lost on numeric details: %q", got)
	}
	// Valores escalares viram texto chave a chave (antes o mapa inteiro era
	// descartado por causa dos números).
	for _, want := range []string{"current: 3", "limit: 3", "plan: FREE"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

// Erros de cluster/banco (throwAppError) chegam com o código em `error`, sem
// `code`, e `details` misturando strings com os booleanos actionable/retryable.
// O código tem que virar ErrorCode e as strings não podem se perder.
func TestAPIError_ClusterEnvelope(t *testing.T) {
	c := &Client{}
	err := c.parseError(respWith(409, `{"statusCode":409,"error":"DB_EXTENSION_IN_USE","message":"cannot drop extension postgis","details":{"extension":"postgis","dependents":"column geom of table places depends on type geometry","actionable":true,"retryable":false},"requestId":"req-z"}`))
	if got := ErrorCode(err); got != "DB_EXTENSION_IN_USE" {
		t.Fatalf("ErrorCode = %q, want DB_EXTENSION_IN_USE", got)
	}
	ae, ok := err.(*APIError)
	if !ok {
		t.Fatalf("not an APIError: %T", err)
	}
	if ae.Details["dependents"] != "column geom of table places depends on type geometry" || ae.Details["extension"] != "postgis" {
		t.Fatalf("details perdidos: %#v", ae.Details)
	}
	if _, has := ae.Details["actionable"]; has {
		t.Fatalf("metadado actionable vazou pra details: %#v", ae.Details)
	}
	if _, has := ae.Details["retryable"]; has {
		t.Fatalf("metadado retryable vazou pra details: %#v", ae.Details)
	}
}

// O classname legado em `error` NÃO é código: só UPPER_SNAKE é promovido.
func TestAPIError_LegacyClassnameIsNotACode(t *testing.T) {
	c := &Client{}
	err := c.parseError(respWith(409, `{"statusCode":409,"error":"ConflictError","message":"busy"}`))
	if got := ErrorCode(err); got != "" {
		t.Fatalf("ErrorCode = %q, want empty", got)
	}
	// E um code explícito vence o `error`.
	err = c.parseError(respWith(403, `{"statusCode":403,"error":"SOME_CODE","code":"NOT_A_MEMBER","message":"x"}`))
	if got := ErrorCode(err); got != "NOT_A_MEMBER" {
		t.Fatalf("ErrorCode = %q, want NOT_A_MEMBER", got)
	}
}

func TestAPIError_ArrayDetails_NoRegression(t *testing.T) {
	c := &Client{}
	// details as string[] — must not break message parsing.
	err := c.parseError(respWith(400, `{"statusCode":400,"message":"Bad input","details":["a","b"],"requestId":"req-y"}`))
	got := err.Error()
	if !strings.Contains(got, "Bad input") || !strings.Contains(got, "req-y") {
		t.Fatalf("message/requestId lost on array details: %q", got)
	}
}

// O `code` do catálogo é o único campo estável para ramificar: `message` é texto
// de UI (traduzível, reescrito sem aviso) e `error` é o classname legado. Antes
// o CLI descartava o code e só podia comparar mensagem.
func TestAPIError_CapturesStableCode(t *testing.T) {
	c := &Client{}
	err := c.parseError(respWith(403, `{"statusCode":403,"error":"ForbiddenError","code":"NOT_A_MEMBER","message":"You are not a member of this workspace"}`))
	if got := ErrorCode(err); got != "NOT_A_MEMBER" {
		t.Fatalf("ErrorCode = %q, want NOT_A_MEMBER", got)
	}
	if got := StatusCode(err); got != 403 {
		t.Fatalf("StatusCode = %d, want 403", got)
	}
}

// ErrorCode/StatusCode precisam atravessar os wraps de fmt.Errorf("%w") que os
// call-sites adicionam para dar contexto — senão o tratamento por código só
// funcionaria na camada que fez a chamada.
func TestAPIError_CodeSurvivesWrapping(t *testing.T) {
	c := &Client{}
	err := c.parseError(respWith(400, `{"statusCode":400,"code":"IMPERSONATION_ACTIVE","message":"Exit impersonation"}`))
	wrapped := fmt.Errorf("switch workspace: %w", err)
	if got := ErrorCode(wrapped); got != "IMPERSONATION_ACTIVE" {
		t.Fatalf("ErrorCode através de wrap = %q", got)
	}
}

// Erro que não veio da API (rede, parse) não tem código — os call-sites usam
// isso para cair no tratamento genérico em vez de casar com o zero-value.
func TestAPIError_CodeAbsentForNonAPIError(t *testing.T) {
	if got := ErrorCode(errors.New("connection refused")); got != "" {
		t.Fatalf("ErrorCode de erro não-API = %q, want vazio", got)
	}
	if got := StatusCode(nil); got != 0 {
		t.Fatalf("StatusCode(nil) = %d, want 0", got)
	}
}

func TestAPIError_NoDetails(t *testing.T) {
	c := &Client{}
	err := c.parseError(respWith(404, `{"statusCode":404,"message":"Not found","requestId":"req-z"}`))
	got := err.Error()
	if got != "API error 404: Not found (requestId: req-z)" {
		t.Fatalf("unexpected: %q", got)
	}
}
