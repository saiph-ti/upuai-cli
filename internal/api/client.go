package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func (c *Client) doRequest(method, path string, body any, result any) error {
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

	if resp.StatusCode == http.StatusUnauthorized && c.credStore != nil {
		if refreshed := c.tryRefreshToken(); refreshed {
			return c.doRequest(method, path, body, result)
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
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(body, &parsed) == nil {
			if parsed.Message != "" {
				apiErr.Message = parsed.Message
			} else if parsed.Error != "" {
				apiErr.Message = parsed.Error
			}
		}
	}
	return apiErr
}

func (c *Client) getToken() string {
	if c.credStore != nil {
		return c.credStore.GetToken()
	}
	return ""
}

func (c *Client) tryRefreshToken() bool {
	if c.credStore == nil {
		return false
	}
	creds, err := c.credStore.Load()
	if err != nil || creds == nil || creds.RefreshToken == "" {
		return false
	}

	url := c.baseURL + "/auth/refresh?refreshToken=" + creds.RefreshToken
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return false
	}
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
	return c.doRequest(http.MethodGet, path, nil, result)
}

func (c *Client) Post(path string, body any, result any) error {
	return c.doRequest(http.MethodPost, path, body, result)
}

func (c *Client) Put(path string, body any, result any) error {
	return c.doRequest(http.MethodPut, path, body, result)
}

func (c *Client) Patch(path string, body any, result any) error {
	return c.doRequest(http.MethodPatch, path, body, result)
}

func (c *Client) Delete(path string) error {
	return c.doRequest(http.MethodDelete, path, nil, nil)
}

// StreamSSE streams a Server-Sent Events endpoint, invoking onLine for each
// `data:` line received. The stream ends when the server sends `event: end`
// or closes the connection. Cancel the context to abort early.
//
// Auth: Bearer header (the API's sse-auth.ts also accepts this; ?access_token=
// is the EventSource fallback for browsers).
func (c *Client) StreamSSE(ctx context.Context, path string, onLine func(string)) error {
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

	if resp.StatusCode == http.StatusUnauthorized && c.credStore != nil {
		if refreshed := c.tryRefreshToken(); refreshed {
			return c.StreamSSE(ctx, path, onLine)
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

	if resp.StatusCode == http.StatusUnauthorized && c.credStore != nil {
		if refreshed := c.tryRefreshToken(); refreshed {
			return c.GetRaw(path)
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
