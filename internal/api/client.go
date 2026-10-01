package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/pkg/version"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	credStore  *config.CredentialStore
}

func NewClient() *Client {
	return &Client{
		baseURL: config.GetAPIURL(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		credStore: config.NewCredentialStore(),
	}
}

func (c *Client) doRequest(method, path string, body any, result any, retried bool) error {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "upuai-cli/"+version.Short())

	token := c.getToken()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Retry no máximo UMA vez após refresh — `retried` impede recursão infinita
	// se o servidor devolver um token que segue dando 401 (token revogado, clock
	// skew, bug). Sem essa guarda o CLI travaria em loop martelando /auth/refresh.
	if resp.StatusCode == http.StatusUnauthorized && !retried && c.credStore != nil {
		if refreshed := c.tryRefreshToken(); refreshed {
			return c.doRequest(method, path, body, result, true)
		}
	}

	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}

	if result != nil && resp.StatusCode != http.StatusNoContent {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}
		if len(respBody) > 0 {
			if err := json.Unmarshal(respBody, result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
		}
	}

	return nil
}

func (c *Client) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Message:    http.StatusText(resp.StatusCode),
	}
	if len(body) > 0 {
		var parsed struct {
			Message   string          `json:"message"`
			Error     string          `json:"error"`
			Code      string          `json:"code"`
			RequestID string          `json:"requestId"`
			Details   json.RawMessage `json:"details"`
		}
		if json.Unmarshal(body, &parsed) == nil {
			if parsed.Message != "" {
				apiErr.Message = parsed.Message
			} else if parsed.Error != "" {
				apiErr.Message = parsed.Error
			}
			apiErr.Code = parsed.Code
			// A API tem DOIS envelopes de erro estável: o catálogo geral manda o
			// código em `code` (ex: NOT_A_MEMBER), e os códigos de cluster/banco
			// (throwAppError — ex: DB_EXTENSION_IN_USE) vêm em `error`, sem `code`.
			// `error` também carrega o classname legado (ValidationError,
			// ConflictError), que NÃO é código — por isso só o formato
			// UPPER_SNAKE é promovido.
			if apiErr.Code == "" && stableErrorCode.MatchString(parsed.Error) {
				apiErr.Code = parsed.Error
			}
			apiErr.RequestID = parsed.RequestID
			apiErr.Details = decodeDetails(parsed.Details)
		}
	}
	return apiErr
}

// stableErrorCode reconhece um código de catálogo (UPPER_SNAKE) em `error`.
var stableErrorCode = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+$`)

// detailsMetaKeys são metadados de classificação que a API anexa a todo erro de
// cluster (throwAppError) — úteis pra máquina, ruído na mensagem pro humano.
var detailsMetaKeys = map[string]bool{"actionable": true, "retryable": true}

// decodeDetails lê `details` (Record<string, unknown> | string[]) chave a chave:
// strings, números e booleanos viram texto; objetos, arrays e os metadados de
// classificação são ignorados. Antes o decode era all-or-nothing em
// map[string]string — um único valor não-string (os booleanos actionable/
// retryable dos erros de cluster, os números de PlanLimit) descartava o mapa
// inteiro, inclusive o `dependents` que explica por que a extensão não sai.
// Nunca falha: details malformado não pode custar message/requestId.
func decodeDetails(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil // string[] ou outro shape — sem detalhe field-level
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if detailsMetaKeys[k] {
			continue
		}
		switch x := v.(type) {
		case string:
			out[k] = x
		case float64:
			out[k] = strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			out[k] = strconv.FormatBool(x)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *Client) getToken() string {
	if c.credStore != nil {
		return c.credStore.GetToken()
	}
	return ""
}

func (c *Client) tryRefreshToken() bool {
	// A scoped machine/CI token (UPUAI_TOKEN) is opaque and long-lived — there is
	// no refresh token to rotate. A 401 means it was revoked or expired; surface
	// it instead of silently falling back to the interactive user's credentials.
	if config.MachineTokenFromEnv() != "" {
		return false
	}
	if c.credStore == nil {
		return false
	}
	creds, err := c.credStore.Load()
	if err != nil || creds == nil || creds.RefreshToken == "" {
		return false
	}

	// Refresh token vai no CORPO do POST, não na query string — na URL ele
	// vazaria em access log de proxy/Traefik. O /auth/refresh só aceita body
	// (o fallback de query saiu da API em 2026-09-15).
	url := c.baseURL + "/auth/refresh"
	payload, err := json.Marshal(map[string]string{"refreshToken": creds.RefreshToken})
	if err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	var refreshResp struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&refreshResp); err != nil {
		return false
	}

	creds.Token = refreshResp.Token
	if refreshResp.RefreshToken != "" {
		creds.RefreshToken = refreshResp.RefreshToken
	}
	_ = c.credStore.Save(creds)
	return true
}

func (c *Client) Get(path string, result any) error {
	return c.doRequest(http.MethodGet, path, nil, result, false)
}

func (c *Client) Post(path string, body any, result any) error {
	return c.doRequest(http.MethodPost, path, body, result, false)
}

func (c *Client) Put(path string, body any, result any) error {
	return c.doRequest(http.MethodPut, path, body, result, false)
}

func (c *Client) Patch(path string, body any, result any) error {
	return c.doRequest(http.MethodPatch, path, body, result, false)
}

func (c *Client) Delete(path string) error {
	return c.doRequest(http.MethodDelete, path, nil, nil, false)
}

// DeleteJSON é o DELETE cujo corpo de resposta interessa (ex: a lista de
// extensões depois de um DROP EXTENSION). Delete continua descartando o corpo.
func (c *Client) DeleteJSON(path string, result any) error {
	return c.doRequest(http.MethodDelete, path, nil, result, false)
}

// withTimeout devolve uma cópia do client com outro teto de tempo por request,
// para chamadas que o servidor sabidamente segura mais que o default de 30s.
// A cópia compartilha baseURL e credStore (o refresh de token continua valendo).
func (c *Client) withTimeout(d time.Duration) *Client {
	clone := *c
	clone.httpClient = &http.Client{Timeout: d}
	return &clone
}

// StreamSSE streams a Server-Sent Events endpoint, invoking onLine for each
// `data:` line received. The stream ends when the server sends `event: end`
// or closes the connection. Cancel the context to abort early.
//
// Auth: Bearer header (the API's sse-auth.ts also accepts this; ?access_token=
// is the EventSource fallback for browsers).
func (c *Client) StreamSSE(ctx context.Context, path string, onLine func(string)) error {
	return c.streamSSE(ctx, path, onLine, false)
}

func (c *Client) streamSSE(ctx context.Context, path string, onLine func(string), retried bool) error {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", "upuai-cli/"+version.Short())

	token := c.getToken()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// SSE streams have no fixed length — bypass the default 30s timeout.
	streamClient := &http.Client{Timeout: 0}
	resp, err := streamClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Retry no máximo UMA vez após refresh (ver doRequest).
	if resp.StatusCode == http.StatusUnauthorized && !retried && c.credStore != nil {
		if refreshed := c.tryRefreshToken(); refreshed {
			return c.streamSSE(ctx, path, onLine, true)
		}
	}

	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Server signals normal completion with `event: end` followed by `data: ok`.
		if strings.HasPrefix(line, "event: end") {
			return nil
		}
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			onLine(data)
		}
	}
	return scanner.Err()
}

func (c *Client) GetRaw(path string) ([]byte, error) {
	return c.getRaw(path, false)
}

func (c *Client) getRaw(path string, retried bool) ([]byte, error) {
	url := c.baseURL + path

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "upuai-cli/"+version.Short())

	token := c.getToken()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Retry no máximo UMA vez após refresh (ver doRequest).
	if resp.StatusCode == http.StatusUnauthorized && !retried && c.credStore != nil {
		if refreshed := c.tryRefreshToken(); refreshed {
			return c.getRaw(path, true)
		}
	}

	if resp.StatusCode >= 400 {
		return nil, c.parseError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return body, nil
}
