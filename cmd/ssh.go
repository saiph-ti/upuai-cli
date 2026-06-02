package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/upuai-cloud/cli/internal/config"
)

var sshCmd = &cobra.Command{
	Use:   "ssh [-s SERVICE] [-- COMMAND...]",
	Short: "Open an interactive shell (or run a command) in the running service container",
	Long: `Open an interactive PTY session inside the running container of your linked
service (or another via -s) — like "railway ssh" / "fly ssh console". Generic:
run any program, or a shell if no command is given.

The "--" separator is optional; everything after the first non-flag argument is
forwarded verbatim as the command to run in the container.

Use --process to target a single process of a multi-process service (default:
web; see "upuai ps").

Examples:
  upuai ssh                          # interactive shell in the linked service
  upuai ssh -s api                   # shell in service "api"
  upuai ssh --process worker         # shell in the "worker" process
  upuai ssh -s api -- bin/rails console
  upuai ssh -- python manage.py shell
  upuai ssh -- node`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		serviceRef, process, command, showHelp, err := parseSSHArgs(args)
		if err != nil {
			return err
		}
		if showHelp {
			return cmd.Help()
		}
		if err := requireAuth(); err != nil {
			return err
		}
		envID, serviceID, err := resolveServiceContext(serviceRef)
		if err != nil {
			return err
		}
		return runSSH(envID, serviceID, process, command)
	},
}

// parseSSHArgs separa as flags próprias do `ssh` (-s/--service, --process, -h) do
// comando a rodar no container. Igual ao parseRunArgs: tudo após "--" (ou após o
// primeiro não-flag) é o comando, verbatim. DisableFlagParsing impede o cobra de
// comer flags destinadas ao programa remoto (ex: `rails console -e production`).
func parseSSHArgs(args []string) (serviceRef, process string, command []string, showHelp bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return serviceRef, process, args[i+1:], false, nil
		}
		if a == "-h" || a == "--help" {
			return "", "", nil, true, nil
		}
		// --process is ssh-specific (not a persistent flag), so it is handled here
		// rather than in consumeLeadingFlag. Supports "--process name" and
		// "--process=name".
		if a == "--process" {
			if i+1 >= len(args) {
				return "", "", nil, false, fmt.Errorf("flag --process requires a value")
			}
			process = args[i+1]
			i++
			continue
		}
		if v, ok := strings.CutPrefix(a, "--process="); ok {
			process = v
			continue
		}
		// Consume upuai's own leading flags (-p/-e/-o/-s/-y/-v incl. =forms). They
		// would otherwise be swallowed into the command because DisableFlagParsing is
		// on (cobra won't parse the persistent -p/-e here).
		if consumed, matched, ferr := consumeLeadingFlag(args, i, &serviceRef); matched {
			if ferr != nil {
				return "", "", nil, false, ferr
			}
			i += consumed - 1
			continue
		}
		// First non-flag (or unknown flag) → everything from here is the command,
		// forwarded verbatim (so `rails console -e production` keeps its own flags).
		return serviceRef, process, args[i:], false, nil
	}
	return serviceRef, process, command, false, nil
}

// runSSH dials the API's exec WebSocket and bridges the local terminal to the
// remote PTY. Framing (mesma do orchestrator): binary = stdin/stdout; text JSON
// = controle ({"type":"resize",...} no sentido cliente→server e {"type":"exit",
// "code"|"error"} no sentido server→cliente).
func runSSH(envID, serviceID, process string, command []string) error {
	token := config.NewCredentialStore().GetToken()
	if token == "" {
		return fmt.Errorf("not authenticated — run `upuai login`")
	}

	// http(s):// → ws(s):// (replace só do prefixo; "https" → "wss").
	wsBase := strings.Replace(config.GetAPIURL(), "http", "ws", 1)
	u, err := url.Parse(fmt.Sprintf("%s/environments/%s/services/%s/instance/exec", wsBase, envID, serviceID))
	if err != nil {
		return fmt.Errorf("invalid API URL: %w", err)
	}
	q := u.Query()
	for _, c := range command {
		q.Add("command", c)
	}
	if process != "" {
		q.Set("process", process)
	}
	u.RawQuery = q.Encode()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)

	conn, resp, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("failed to open exec session (%s): %w", resp.Status, err)
		}
		return fmt.Errorf("failed to open exec session: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Raw terminal: repassa cada tecla (incl. Ctrl-C) pro processo remoto sem o
	// terminal local interpretar. Restaurado no final.
	fd := int(os.Stdin.Fd())
	var oldState *term.State
	if term.IsTerminal(fd) {
		oldState, err = term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("failed to set raw terminal mode: %w", err)
		}
		defer func() { _ = term.Restore(fd, oldState) }()
	}

	var writeMu sync.Mutex
	exitCode := 0
	done := make(chan struct{})

	// Resize inicial antes de qualquer outro write (single-thread aqui).
	sendResize(conn, fd, &writeMu)

	// Reader: server → stdout / controle.
	go func() {
		defer close(done)
		for {
			mt, data, rerr := conn.ReadMessage()
			if rerr != nil {
				return
			}
			switch mt {
			case websocket.BinaryMessage:
				_, _ = os.Stdout.Write(data)
			case websocket.TextMessage:
				var ctrl struct {
					Type  string `json:"type"`
					Code  *int   `json:"code"`
					Error string `json:"error"`
				}
				if json.Unmarshal(data, &ctrl) == nil && ctrl.Type == "exit" {
					switch {
					case ctrl.Error != "":
						fmt.Fprintf(os.Stderr, "\r\n[upuai] exec error: %s\r\n", ctrl.Error)
						exitCode = 1
					case ctrl.Code != nil:
						exitCode = *ctrl.Code
					}
				}
			}
		}
	}()

	// Stdin pump: stdin local → binary frames.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := os.Stdin.Read(buf)
			if n > 0 {
				writeMu.Lock()
				werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n])
				writeMu.Unlock()
				if werr != nil {
					return
				}
			}
			if rerr != nil {
				// EOF (Ctrl-D) → fecha a escrita pro server propagar ao processo.
				writeMu.Lock()
				_ = conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				writeMu.Unlock()
				return
			}
		}
	}()

	// Resize poll: cross-platform (sem SIGWINCH, que não existe no Windows).
	// 300ms é responsivo o bastante pra um console e barato.
	go func() {
		lastW, lastH := 0, 0
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if w, h, gerr := term.GetSize(fd); gerr == nil && (w != lastW || h != lastH) {
					lastW, lastH = w, h
					sendResizeWH(conn, w, h, &writeMu)
				}
			}
		}
	}()

	<-done

	if oldState != nil {
		_ = term.Restore(fd, oldState)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

// sendResize lê o tamanho atual do terminal e envia o frame de resize.
func sendResize(conn *websocket.Conn, fd int, mu *sync.Mutex) {
	w, h, err := term.GetSize(fd)
	if err != nil || w <= 0 || h <= 0 {
		return
	}
	sendResizeWH(conn, w, h, mu)
}

func sendResizeWH(conn *websocket.Conn, w, h int, mu *sync.Mutex) {
	payload, err := json.Marshal(map[string]any{"type": "resize", "cols": w, "rows": h})
	if err != nil {
		return
	}
	mu.Lock()
	_ = conn.WriteMessage(websocket.TextMessage, payload)
	mu.Unlock()
}

func init() {
	// Sem flags registradas no cobra: DisableFlagParsing está on e o parsing é
	// manual (parseSSHArgs) pra não comer flags do programa remoto. O help
	// documenta -s/--service.
	rootCmd.AddCommand(sshCmd)
}
