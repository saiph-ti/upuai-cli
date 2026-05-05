package api

import "fmt"

type APIError struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
	Path       string `json:"path,omitempty"`
	// RequestID vem do Fastify error-handler quando disponível. Em prod a API
	// mascara `message` ("Internal server error") e o RequestID é a única
	// âncora que o usuário tem pra abrir suporte sem ler stack trace.
	RequestID string `json:"requestId,omitempty"`
}

func (e *APIError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("API error %d: %s (requestId: %s)", e.StatusCode, e.Message, e.RequestID)
	}
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
}
