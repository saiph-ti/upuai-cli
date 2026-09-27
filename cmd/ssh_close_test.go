package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// execServer sobe um endpoint de exec falso e aponta o CLI pra ele. `session`
// recebe a conexão já aberta e decide como a sessão termina.
func execServer(t *testing.T, session func(conn *websocket.Conn, r *http.Request)) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		session(conn, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("UPUAI_API_URL", srv.URL)
	t.Setenv("UPUAI_TOKEN", "upua_test")
}

func closeWith(conn *websocket.Conn, code int, reason string) {
	_ = conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
}

func runNonTTY(command ...string) error {
	noTTY := false
	return runSSH("env-1", "svc-1", sshOptions{tty: &noTTY, command: command})
}

func TestRunSSH_ExitFrameEndsTheSessionCleanly(t *testing.T) {
	var query string
	execServer(t, func(conn *websocket.Conn, r *http.Request) {
		query = r.URL.RawQuery
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"exit","code":0}`))
		closeWith(conn, websocket.CloseNormalClosure, "exited")
	})

	if err := runNonTTY("true"); err != nil {
		t.Fatalf("runSSH = %v, want nil", err)
	}
	for _, want := range []string{"tty=false", "stdin=", "command=true"} {
		if !strings.Contains(query, want) {
			t.Errorf("exec query %q is missing %q", query, want)
		}
	}
}

// Sessão que termina sem o frame de exit é falha — antes o CLI saía 0 e mudo.
func TestRunSSH_CloseWithoutExitFrameIsAnError(t *testing.T) {
	cases := []struct {
		name    string
		session func(conn *websocket.Conn, r *http.Request)
		want    string
	}{
		{
			name: "acesso negado pela API",
			session: func(conn *websocket.Conn, _ *http.Request) {
				closeWith(conn, websocket.ClosePolicyViolation, "forbidden")
			},
			want: "exec session refused",
		},
		{
			name: "upstream do orchestrator falhou",
			session: func(conn *websocket.Conn, _ *http.Request) {
				closeWith(conn, websocket.CloseInternalServerErr, "orchestrator exec error")
			},
			want: "orchestrator exec error",
		},
		{
			name: "fechamento normal sem status",
			session: func(conn *websocket.Conn, _ *http.Request) {
				closeWith(conn, websocket.CloseNormalClosure, "")
			},
			want: "close code 1000",
		},
		{
			name:    "conexão derrubada sem close frame",
			session: func(_ *websocket.Conn, _ *http.Request) {},
			want:    "exec session lost",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			execServer(t, tc.session)

			err := runNonTTY("true")
			if err == nil {
				t.Fatal("runSSH = nil, want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("runSSH error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// openStdinPipe troca o stdin do processo por um pipe que fica aberto — o stdin
// de um runner que cria o pipe e nunca escreve nem fecha. Devolve o lado de
// escrita, pro teste decidir quando (e se) a entrada acaba.
func openStdinPipe(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = prev
		_ = w.Close()
		_ = r.Close()
	})
	return w
}

// captureStderr devolve o que foi escrito no stderr enquanto fn rodava.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	fn()
	os.Stderr = prev
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

func shortStdinHintDelay(t *testing.T) {
	t.Helper()
	prev := stdinHintDelay
	stdinHintDelay = 20 * time.Millisecond
	t.Cleanup(func() { stdinHintDelay = prev })
}

// exitAfter segura a sessão aberta por d e então encerra com exit 0.
func exitAfter(d time.Duration) func(conn *websocket.Conn, r *http.Request) {
	return func(conn *websocket.Conn, _ *http.Request) {
		time.Sleep(d)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"exit","code":0}`))
		closeWith(conn, websocket.CloseNormalClosure, "exited")
	}
}

// Stdin aberto e mudo: a sessão espera um EOF que pode nunca vir. Em vez de
// ficar parada em silêncio, avisa como sair.
func TestRunSSH_HintsWhenStdinStaysOpenAndSilent(t *testing.T) {
	shortStdinHintDelay(t)
	openStdinPipe(t)
	execServer(t, exitAfter(200*time.Millisecond))

	stderr := captureStderr(t, func() {
		if err := runNonTTY("true"); err != nil {
			t.Errorf("runSSH = %v, want nil", err)
		}
	})

	if !strings.Contains(stderr, stdinWaitHint) {
		t.Errorf("stderr = %q, want the stdin hint", stderr)
	}
}

func TestRunSSH_NoHintWhenStdinEnds(t *testing.T) {
	shortStdinHintDelay(t)
	w := openStdinPipe(t)
	_, _ = w.WriteString("SELECT 1;\n")
	_ = w.Close()
	execServer(t, exitAfter(200*time.Millisecond))

	stderr := captureStderr(t, func() {
		if err := runNonTTY("psql"); err != nil {
			t.Errorf("runSSH = %v, want nil", err)
		}
	})

	if stderr != "" {
		t.Errorf("stderr = %q, want nothing: the input ended", stderr)
	}
}

// -n não anexa stdin: nada a esperar, nada a avisar, e o servidor é informado.
func TestRunSSH_NoStdinFlagSkipsAnOpenPipe(t *testing.T) {
	shortStdinHintDelay(t)
	openStdinPipe(t)
	var query string
	execServer(t, func(conn *websocket.Conn, r *http.Request) {
		query = r.URL.RawQuery
		exitAfter(200*time.Millisecond)(conn, r)
	})

	stderr := captureStderr(t, func() {
		if err := runSSH("env-1", "svc-1", sshOptions{noStdin: true, command: []string{"true"}}); err != nil {
			t.Errorf("runSSH = %v, want nil", err)
		}
	})

	if stderr != "" {
		t.Errorf("stderr = %q, want nothing", stderr)
	}
	for _, want := range []string{"tty=false", "stdin=false"} {
		if !strings.Contains(query, want) {
			t.Errorf("exec query %q is missing %q", query, want)
		}
	}
}

// Comando que já está mandando saída está rodando: o stdin aberto não o segura,
// e o aviso seria falso.
func TestRunSSH_NoHintWhenTheCommandIsAlreadyProducingOutput(t *testing.T) {
	shortStdinHintDelay(t)
	openStdinPipe(t)
	execServer(t, func(conn *websocket.Conn, r *http.Request) {
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte("\x02working\n"))
		exitAfter(200*time.Millisecond)(conn, r)
	})

	stderr := captureStderr(t, func() {
		if err := runNonTTY("long-import"); err != nil {
			t.Errorf("runSSH = %v, want nil", err)
		}
	})

	if stderr != "working\n" {
		t.Errorf("stderr = %q, want only the command's own output", stderr)
	}
}
