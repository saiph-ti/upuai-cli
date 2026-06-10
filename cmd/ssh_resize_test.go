package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestSendResizeWH locks the resize wire contract the orchestrator's PTY exec
// endpoint expects: a TextMessage JSON frame {"type":"resize","cols":W,"rows":H}.
// Covers tech-debt db-version-ship-followups #5 (sendResize sem cobertura).
func TestSendResizeWH(t *testing.T) {
	type resizeFrame struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}

	frames := make(chan resizeFrame, 4)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			msgType, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType != websocket.TextMessage {
				t.Errorf("expected TextMessage frame, got type %d", msgType)
				return
			}
			var f resizeFrame
			if err := json.Unmarshal(payload, &f); err != nil {
				t.Errorf("frame is not JSON: %v (%s)", err, payload)
				return
			}
			frames <- f
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	var mu sync.Mutex

	// Frames concorrentes (SIGWINCH em rajada) — o mutex serializa os writes;
	// sem ele o gorilla/websocket entra em pânico com writer concorrente.
	var wg sync.WaitGroup
	sizes := [][2]int{{120, 40}, {80, 24}, {200, 50}}
	for _, s := range sizes {
		wg.Add(1)
		go func(w, h int) {
			defer wg.Done()
			sendResizeWH(conn, w, h, &mu)
		}(s[0], s[1])
	}
	wg.Wait()

	got := map[[2]int]bool{}
	for range sizes {
		select {
		case f := <-frames:
			if f.Type != "resize" {
				t.Fatalf(`expected type "resize", got %q`, f.Type)
			}
			got[[2]int{f.Cols, f.Rows}] = true
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for resize frames")
		}
	}
	for _, s := range sizes {
		if !got[s] {
			t.Errorf("resize frame %dx%d never arrived", s[0], s[1])
		}
	}
}

// sendResize com fd inválido não pode emitir frame nenhum (term.GetSize falha
// → silêncio, nunca um resize 0x0 que quebraria o PTY remoto).
func TestSendResize_InvalidFDEmitsNothing(t *testing.T) {
	received := make(chan []byte, 1)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, payload, err := conn.ReadMessage(); err == nil {
			received <- payload
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	var mu sync.Mutex
	sendResize(conn, -1, &mu)
	_ = conn.Close()

	select {
	case payload := <-received:
		t.Fatalf("expected no frame for invalid fd, got %s", payload)
	case <-time.After(300 * time.Millisecond):
		// silêncio = correto
	}
}
