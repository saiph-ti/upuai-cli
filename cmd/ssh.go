package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

By default a PTY is allocated only when stdin and stdout are terminals; in a pipe
or redirect the output is streamed byte-exact (separate stdout/stderr, no terminal
cooking) — so scripting works. Force it with -t/--tty or disable it with
-T/--no-tty (parity with "ssh -t/-T").

Without a PTY, piped stdin is forwarded to the command, which may only start
once that input ends. If your runner (CI, agent, supervisor) leaves stdin open
and the command reads nothing, pass -n/--no-stdin (parity with "ssh -n") or
redirect it: "upuai ssh -- cmd </dev/null". -n needs a command and never
allocates a PTY.

Examples:
  upuai ssh                          # interactive shell in the linked service
  upuai ssh -s api                   # shell in service "api"
  upuai ssh --process worker         # shell in the "worker" process
  upuai ssh -s api -- bin/rails console
  upuai ssh -- python manage.py shell
  echo "SELECT 1" | upuai ssh -s db -- psql   # non-interactive: stdout returned
  upuai ssh -s api -- cat log.txt > out.txt    # byte-exact, no PTY
  upuai ssh -t -- top                          # force a PTY
  upuai ssh -n -- php artisan migrate --force  # CI/agent: never wait on stdin
  upuai ssh -- node`,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts, err := parseSSHArgs(args)
		if err != nil {
			return err
		}
		if opts.showHelp {
			return cmd.Help()
		}
		if err := requireAuth(); err != nil {
			return err
		}
		envID, serviceID, err := resolveServiceContext(opts.serviceRef)
		if err != nil {
			return err
		}
		return runSSH(envID, serviceID, opts)
	},
}

// sshOptions é o que o `ssh` tira da linha de comando antes do comando remoto.
type sshOptions struct {
	serviceRef string
	process    string
	tty        *bool // nil = auto; -t/-T forçam
	noStdin    bool  // -n: não anexa stdin, mesmo com um pipe na entrada
	command    []string
	showHelp   bool
}

// parseSSHArgs separa as flags próprias do `ssh` (-s/--service, --process, -t/-T,
// -n, -h) do comando a rodar no container. Igual ao parseRunArgs: tudo após "--" (ou após o
// primeiro não-flag) é o comando, verbatim. DisableFlagParsing impede o cobra de
// comer flags destinadas ao programa remoto (ex: `rails console -e production`).
func parseSSHArgs(args []string) (sshOptions, error) {
	var opts sshOptions
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			opts.command = args[i+1:]
			return opts, nil
		}
		if a == "-h" || a == "--help" {
			return sshOptions{showHelp: true}, nil
		}
		// -t/--tty força PTY; -T/--no-tty força não-PTY. Sem nenhum, runSSH
		// auto-detecta (stdin+stdout são terminais). São flags do ssh (não
		// persistentes), tratadas aqui como --process. Booleanas: sem valor.
		if a == "-t" || a == "--tty" {
			v := true
			opts.tty = &v
			continue
		}
		if a == "-T" || a == "--no-tty" {
			v := false
			opts.tty = &v
			continue
		}
		// -n/--no-stdin: não anexa stdin (paridade `ssh -n`). Para runner que deixa
		// o stdin aberto sem nunca fechar, onde a sessão esperaria um EOF que não vem.
		if a == "-n" || a == "--no-stdin" {
			opts.noStdin = true
			continue
		}
		// --process is ssh-specific (not a persistent flag), so it is handled here
		// rather than in consumeLeadingFlag. Supports "--process name" and
		// "--process=name".
		if a == "--process" {
			if i+1 >= len(args) {
				return sshOptions{}, fmt.Errorf("flag --process requires a value")
			}
			opts.process = args[i+1]
			i++
			continue
		}
		if v, ok := strings.CutPrefix(a, "--process="); ok {
			opts.process = v
			continue
		}
		// Consume upuai's own leading flags (-p/-e/-o/-s/-y/-v incl. =forms). They
		// would otherwise be swallowed into the command because DisableFlagParsing is
		// on (cobra won't parse the persistent -p/-e here).
		if consumed, matched, ferr := consumeLeadingFlag(args, i, &opts.serviceRef); matched {
			if ferr != nil {
				return sshOptions{}, ferr
			}
			i += consumed - 1
			continue
		}
		// First non-flag (or unknown flag) → everything from here is the command,
		// forwarded verbatim (so `rails console -e production` keeps its own flags).
		opts.command = args[i:]
		return opts, nil
	}
	return opts, nil
}

// resolveExecStreams decide se a sessão aloca PTY e se anexa stdin, a partir das
// flags e do que stdin/stdout são de fato.
//
// TTY: override explícito (-t/-T) ou auto (stdin E stdout são terminais). Em
// pipe/redirect → não-TTY: saída byte-exata, stdout/stderr separados.
//
// stdin: em TTY sempre (o shell precisa). Em não-TTY só com entrada real — pipe
// ou arquivo, não um char device como terminal ou /dev/null. Abrir stdin à toa
// faz alguns containers engolirem a saída (a mesma razão do -i explícito do
// kubectl). -n desliga os dois: sem stdin não há sessão interativa.
func resolveExecStreams(opts sshOptions, stdinIsTerminal, stdoutIsTerminal, stdinHasInput bool) (tty, hasStdin bool, err error) {
	if opts.noStdin {
		if opts.tty != nil && *opts.tty {
			return false, false, fmt.Errorf("-n/--no-stdin cannot be combined with -t/--tty: a PTY session reads from stdin")
		}
		// Sem comando o que roda é um shell, que lê o stdin: sem stdin ele sai na
		// hora, com código 0, parecendo que funcionou.
		if len(opts.command) == 0 {
			return false, false, fmt.Errorf("-n/--no-stdin needs a command to run: an interactive shell reads from stdin")
		}
		return false, false, nil
	}
	tty = stdinIsTerminal && stdoutIsTerminal
	if opts.tty != nil {
		tty = *opts.tty
	}
	return tty, tty || stdinHasInput, nil
}

// stdinHintDelay é quanto a sessão não-TTY espera por um stdin mudo antes de
// avisar. Variável para o teste encurtar.
var stdinHintDelay = 10 * time.Second

const stdinWaitHint = "[upuai] no output yet, and stdin is still open: the command may be waiting for it to end — " +
	"if it reads no input, pass -n (upuai ssh -n -- cmd) or redirect </dev/null"

// runSSH dials the API's exec WebSocket and bridges the local terminal to the
// remote PTY. Framing (mesma do orchestrator): binary = stdin/stdout; text JSON
// = controle ({"type":"resize",...} no sentido cliente→server e {"type":"exit",
// "code"|"error"} no sentido server→cliente).
func runSSH(envID, serviceID string, opts sshOptions) error {
	token := config.NewCredentialStore().GetToken()
	if token == "" {
		return fmt.Errorf("not authenticated — run `upuai login`")
	}

	// Os streams do processo são lidos uma vez, aqui: as goroutines da sessão usam
	// estas cópias e nunca os globais de `os`.
	stdin, stdout, stderr := os.Stdin, os.Stdout, os.Stderr
	hintDelay := stdinHintDelay

	stdinFd := int(stdin.Fd())
	stdinHasInput := false
	if st, statErr := stdin.Stat(); statErr == nil {
		stdinHasInput = st.Mode()&os.ModeCharDevice == 0
	}
	tty, hasStdin, err := resolveExecStreams(opts, term.IsTerminal(stdinFd), term.IsTerminal(int(stdout.Fd())), stdinHasInput)
	if err != nil {
		return err
	}

	u, err := buildExecURL(config.GetAPIURL(), envID, serviceID, opts.process, opts.command, tty, hasStdin)
	if err != nil {
		return fmt.Errorf("invalid API URL: %w", err)
	}

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

	// Raw terminal só em TTY: repassa cada tecla (incl. Ctrl-C) pro processo
	// remoto sem o terminal local interpretar. Restaurado no final. (Com -t
	// forçado sobre um pipe, IsTerminal é falso e MakeRaw é pulado.)
	fd := stdinFd
	var oldState *term.State
	if tty && term.IsTerminal(fd) {
		oldState, err = term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("failed to set raw terminal mode: %w", err)
		}
		defer func() { _ = term.Restore(fd, oldState) }()
	}

	var writeMu sync.Mutex
	exitCode := 0
	// gotExit/readErr são escritos só pelo reader e lidos depois de `done` fechar.
	gotExit := false
	var readErr error
	// gotOutput: o servidor já mandou algum byte de saída do comando.
	var gotOutput atomic.Bool
	done := make(chan struct{})

	// Resize inicial só em TTY (single-thread aqui).
	if tty {
		sendResize(conn, fd, &writeMu)
	}

	// Reader: server → stdout / controle.
	go func() {
		defer close(done)
		for {
			mt, data, rerr := conn.ReadMessage()
			if rerr != nil {
				readErr = rerr
				return
			}
			switch mt {
			case websocket.BinaryMessage:
				gotOutput.Store(true)
				switch {
				case tty:
					// PTY: stream único cru → stdout.
					_, _ = stdout.Write(data)
				case len(data) == 0:
					// frame de canal vazio — ignora
				case data[0] == 0x02:
					_, _ = stderr.Write(data[1:])
				default: // 0x01 e qualquer outro → stdout
					_, _ = stdout.Write(data[1:])
				}
			case websocket.TextMessage:
				var ctrl struct {
					Type  string `json:"type"`
					Code  *int   `json:"code"`
					Error string `json:"error"`
				}
				if json.Unmarshal(data, &ctrl) == nil && ctrl.Type == "exit" {
					gotExit = true
					switch {
					case ctrl.Error != "":
						_, _ = fmt.Fprintf(stderr, "\r\n[upuai] exec error: %s\r\n", ctrl.Error)
						exitCode = 1
					case ctrl.Code != nil:
						exitCode = *ctrl.Code
					}
				}
			}
		}
	}()

	// Stdin pump: stdin local → binary frames. Só roda quando há stdin anexado
	// (hasStdin); senão o servidor faz exec sem stream de stdin.
	// stdinActive: o stdin local já entregou algum byte ou chegou ao fim.
	var stdinActive atomic.Bool
	if hasStdin {
		go func() {
			buf := make([]byte, 4096)
			for {
				n, rerr := stdin.Read(buf)
				if n > 0 || rerr != nil {
					stdinActive.Store(true)
				}
				if n > 0 {
					writeMu.Lock()
					werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n])
					writeMu.Unlock()
					if werr != nil {
						return
					}
				}
				if rerr != nil {
					// EOF do stdin → half-close (frame de controle), sem derrubar a
					// conexão: o server fecha só o stdin do processo remoto (que vê EOF)
					// e o reader continua drenando stdout/stderr até o frame de exit.
					// (Antes mandava CloseMessage, que derrubava a sessão antes do flush
					// → stdout vazio em pipe.)
					writeMu.Lock()
					_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"stdin_eof"}`))
					writeMu.Unlock()
					return
				}
			}
		}()
	}

	// Sem PTY, o comando pode só começar quando o stdin acabar. Um stdin aberto
	// que nunca entrega nada (runner que cria o pipe e esquece) deixaria a sessão
	// parada e muda — avisa uma vez, no stderr, como sair dela. Comando que já
	// produz saída está rodando: não há o que avisar.
	if hasStdin && !tty {
		go func() {
			select {
			case <-done:
			case <-time.After(hintDelay):
				if !stdinActive.Load() && !gotOutput.Load() {
					_, _ = fmt.Fprintln(stderr, stdinWaitHint)
				}
			}
		}()
	}

	// Resize poll só em TTY: cross-platform (sem SIGWINCH, que não existe no
	// Windows). 300ms é responsivo o bastante pra um console e barato.
	if tty {
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
	}

	<-done

	if oldState != nil {
		_ = term.Restore(fd, oldState)
	}
	if !gotExit {
		return execClosedError(readErr)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

// execClosedError explica uma sessão que terminou sem o frame de exit: o servidor
// fechou (ou a conexão caiu) antes de o comando reportar o status. Sem isso o CLI
// saía 0 e mudo — indistinguível de um comando que rodou e não imprimiu nada.
func execClosedError(err error) error {
	var closeErr *websocket.CloseError
	// 1006 não é um close do servidor: é como o gorilla reporta a conexão que caiu
	// sem close frame.
	if !errors.As(err, &closeErr) || closeErr.Code == websocket.CloseAbnormalClosure {
		return fmt.Errorf("exec session lost before the command reported an exit status: %w", err)
	}
	if closeErr.Code == websocket.ClosePolicyViolation {
		return fmt.Errorf("exec session refused: access denied, or the service does not exist in this environment")
	}
	reason := closeErr.Text
	if reason == "" {
		reason = fmt.Sprintf("close code %d", closeErr.Code)
	}
	return fmt.Errorf("exec session closed by the server before the command reported an exit status (%s)", reason)
}

// buildExecURL monta a URL do WebSocket de exec (http(s)→ws(s), command repetido,
// process opcional, tty/stdin explícitos). Extraída pra ser testável sem conexão
// real. `tty` e `stdin` são sempre enviados pelo CLI novo; se ausentes, o
// orchestrator assume ambos true (compat com CLIs antigos).
func buildExecURL(apiURL, envID, serviceID, process string, command []string, tty, stdin bool) (*url.URL, error) {
	// http(s):// → ws(s):// (replace só do prefixo; "https" → "wss").
	wsBase := strings.Replace(apiURL, "http", "ws", 1)
	u, err := url.Parse(fmt.Sprintf("%s/environments/%s/services/%s/instance/exec", wsBase, envID, serviceID))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	for _, c := range command {
		q.Add("command", c)
	}
	if process != "" {
		q.Set("process", process)
	}
	q.Set("tty", strconv.FormatBool(tty))
	q.Set("stdin", strconv.FormatBool(stdin))
	u.RawQuery = q.Encode()
	return u, nil
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
