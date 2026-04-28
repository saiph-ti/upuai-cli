package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/pkg/browser"
)

type OAuthResult struct {
	Code        string
	RedirectURI string
}

func StartOAuthFlow(authURL string) (*OAuthResult, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start local server: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	state, err := generateState()
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	fullURL := fmt.Sprintf("%s?redirect_uri=%s&state=%s", authURL, redirectURI, state)

	resultCh := make(chan *OAuthResult, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errCh <- fmt.Errorf("state mismatch (possible CSRF attack)")
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no authorization code received")
			http.Error(w, "No code", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body style="font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#0A0D14;color:#F0F2F5">
			<div style="text-align:center">
				<h1 style="color:#009C3B">Upuai Cloud</h1>
				<p>Authentication successful! You can close this window.</p>
			</div>
		</body></html>`)

		resultCh <- &OAuthResult{Code: code, RedirectURI: redirectURI}
	})

	server := &http.Server{Handler: mux}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	if err := browser.OpenURL(fullURL); err != nil {
		_ = server.Close()
		return nil, fmt.Errorf("failed to open browser: %w\nPlease open this URL manually:\n%s", err, fullURL)
	}

	select {
	case result := <-resultCh:
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		return result, nil
	case err := <-errCh:
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		return nil, err
	case <-time.After(5 * time.Minute):
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		return nil, fmt.Errorf("authentication timed out after 5 minutes")
	}
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
