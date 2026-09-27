package cmd

import (
	"reflect"
	"testing"
)

// resetLeadingFlagGlobals clears the persistent-flag globals that consumeLeadingFlag
// mutates, so table cases don't leak state into each other.
func resetLeadingFlagGlobals() {
	flagProject = ""
	flagEnvironment = ""
	flagOutput = ""
	flagYes = false
	flagVerbose = false
}

func TestParseSSHArgs(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }
	tests := []struct {
		name        string
		args        []string
		wantSvcRef  string
		wantProcess string
		wantTTY     *bool // nil = auto (nenhum -t/-T)
		wantNoStdin bool
		wantCommand []string
		wantHelp    bool
		wantProject string
		wantEnv     string
	}{
		{
			// The exact client report: -p before -s caused -p/-s to be swallowed into
			// the command (serviceRef="") and ssh fell back to the linked service.
			name:        "global -p before -s and command",
			args:        []string{"-p", "adv-os", "-s", "adv-os-web", "--", "bin/rails", "console"},
			wantSvcRef:  "adv-os-web",
			wantCommand: []string{"bin/rails", "console"},
			wantProject: "adv-os",
		},
		{
			// upuai's own -e is consumed; the remote command's own -e (after --) is kept.
			name:        "own -e consumed, remote -e preserved",
			args:        []string{"-p", "adv-os", "-e", "production", "-s", "web", "--", "rails", "console", "-e", "production"},
			wantSvcRef:  "web",
			wantCommand: []string{"rails", "console", "-e", "production"},
			wantProject: "adv-os",
			wantEnv:     "production",
		},
		{
			name:        "service only, no global flags",
			args:        []string{"-s", "api", "--", "sh"},
			wantSvcRef:  "api",
			wantCommand: []string{"sh"},
		},
		{
			name:        "=forms",
			args:        []string{"--project=adv-os", "--service=web", "--", "node"},
			wantSvcRef:  "web",
			wantCommand: []string{"node"},
			wantProject: "adv-os",
		},
		{
			name:        "no -- separator: first positional starts command",
			args:        []string{"-s", "api", "python", "manage.py", "shell"},
			wantSvcRef:  "api",
			wantCommand: []string{"python", "manage.py", "shell"},
		},
		{
			name:        "no service: command after globals (linked-service fallback)",
			args:        []string{"-p", "adv-os", "--", "sh"},
			wantSvcRef:  "",
			wantCommand: []string{"sh"},
			wantProject: "adv-os",
		},
		{
			name:        "--process consumed, command preserved",
			args:        []string{"-s", "api", "--process", "worker", "--", "sh"},
			wantSvcRef:  "api",
			wantProcess: "worker",
			wantCommand: []string{"sh"},
		},
		{
			name:        "--process=value form",
			args:        []string{"--process=clock", "--", "date"},
			wantProcess: "clock",
			wantCommand: []string{"date"},
		},
		{
			name:        "-t forces tty",
			args:        []string{"-t", "-s", "api", "--", "sh"},
			wantSvcRef:  "api",
			wantTTY:     boolPtr(true),
			wantCommand: []string{"sh"},
		},
		{
			name:        "--tty long form",
			args:        []string{"--tty", "--", "top"},
			wantTTY:     boolPtr(true),
			wantCommand: []string{"top"},
		},
		{
			name:        "-T forces no-tty",
			args:        []string{"-T", "-s", "api", "--", "cat"},
			wantSvcRef:  "api",
			wantTTY:     boolPtr(false),
			wantCommand: []string{"cat"},
		},
		{
			name:        "--no-tty long form",
			args:        []string{"--no-tty", "--", "psql"},
			wantTTY:     boolPtr(false),
			wantCommand: []string{"psql"},
		},
		{
			// -t/-T DEPOIS de -- pertencem ao comando remoto, não ao upuai.
			name:        "-t after -- is command's flag",
			args:        []string{"-s", "api", "--", "rails", "-t"},
			wantSvcRef:  "api",
			wantTTY:     nil,
			wantCommand: []string{"rails", "-t"},
		},
		{
			name:        "-n before the command",
			args:        []string{"-s", "api", "-n", "--", "php", "artisan", "migrate"},
			wantSvcRef:  "api",
			wantNoStdin: true,
			wantCommand: []string{"php", "artisan", "migrate"},
		},
		{
			name:        "--no-stdin long form, no separator",
			args:        []string{"--no-stdin", "ls"},
			wantNoStdin: true,
			wantCommand: []string{"ls"},
		},
		{
			// -n depois do comando é do programa remoto, não do upuai.
			name:        "remote -n preserved",
			args:        []string{"--", "head", "-n", "5", "log.txt"},
			wantCommand: []string{"head", "-n", "5", "log.txt"},
		},
		{
			name:     "help short",
			args:     []string{"-h"},
			wantHelp: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetLeadingFlagGlobals()
			opts, err := parseSSHArgs(tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if opts.showHelp != tc.wantHelp {
				t.Fatalf("help = %v, want %v", opts.showHelp, tc.wantHelp)
			}
			if tc.wantHelp {
				return
			}
			if opts.serviceRef != tc.wantSvcRef {
				t.Errorf("serviceRef = %q, want %q", opts.serviceRef, tc.wantSvcRef)
			}
			if opts.process != tc.wantProcess {
				t.Errorf("process = %q, want %q", opts.process, tc.wantProcess)
			}
			if !reflect.DeepEqual(opts.tty, tc.wantTTY) {
				t.Errorf("tty = %v, want %v", opts.tty, tc.wantTTY)
			}
			if opts.noStdin != tc.wantNoStdin {
				t.Errorf("noStdin = %v, want %v", opts.noStdin, tc.wantNoStdin)
			}
			if !reflect.DeepEqual(opts.command, tc.wantCommand) {
				t.Errorf("command = %#v, want %#v", opts.command, tc.wantCommand)
			}
			if flagProject != tc.wantProject {
				t.Errorf("flagProject = %q, want %q", flagProject, tc.wantProject)
			}
			if flagEnvironment != tc.wantEnv {
				t.Errorf("flagEnvironment = %q, want %q", flagEnvironment, tc.wantEnv)
			}
		})
	}
}

func TestParseSSHArgs_MissingFlagValue(t *testing.T) {
	resetLeadingFlagGlobals()
	if _, err := parseSSHArgs([]string{"-s"}); err == nil {
		t.Fatal("expected error for -s without a value")
	}
	if _, err := parseSSHArgs([]string{"--process"}); err == nil {
		t.Fatal("expected error for --process without a value")
	}
}

// resolveExecStreams é a negociação PTY/stdin. Cada linha é um ambiente real.
func TestResolveExecStreams(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name                       string
		opts                       sshOptions
		stdinTerm, stdoutTerm, inp bool
		wantTTY, wantStdin         bool
	}{
		{name: "terminal interativo", stdinTerm: true, stdoutTerm: true, wantTTY: true, wantStdin: true},
		{name: "agente/CI: stdin /dev/null, stdout capturado", wantTTY: false, wantStdin: false},
		{name: "pipe na entrada", inp: true, wantTTY: false, wantStdin: true},
		{name: "terminal com stdout redirecionado", stdinTerm: true, wantTTY: false, wantStdin: false},
		{name: "-t força PTY sobre pipe", opts: sshOptions{tty: &yes}, inp: true, wantTTY: true, wantStdin: true},
		{name: "-T num terminal", opts: sshOptions{tty: &no}, stdinTerm: true, stdoutTerm: true, wantTTY: false, wantStdin: false},
		{name: "-n ignora o pipe aberto", opts: sshOptions{noStdin: true, command: []string{"ls"}}, inp: true, wantTTY: false, wantStdin: false},
		{name: "-n num terminal não aloca PTY", opts: sshOptions{noStdin: true, command: []string{"ls"}}, stdinTerm: true, stdoutTerm: true, wantTTY: false, wantStdin: false},
		{name: "-n com -T", opts: sshOptions{noStdin: true, tty: &no, command: []string{"ls"}}, inp: true, wantTTY: false, wantStdin: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tty, stdin, err := resolveExecStreams(tc.opts, tc.stdinTerm, tc.stdoutTerm, tc.inp)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tty != tc.wantTTY || stdin != tc.wantStdin {
				t.Errorf("tty=%v stdin=%v, want tty=%v stdin=%v", tty, stdin, tc.wantTTY, tc.wantStdin)
			}
		})
	}

	if _, _, err := resolveExecStreams(sshOptions{noStdin: true, tty: &yes, command: []string{"ls"}}, true, true, false); err == nil {
		t.Error("-n with -t: expected an error, a PTY session reads from stdin")
	}
	// Sem comando roda um shell, que sem stdin sai na hora com código 0.
	if _, _, err := resolveExecStreams(sshOptions{noStdin: true}, true, true, false); err == nil {
		t.Error("-n without a command: expected an error")
	}
}

func TestBuildExecURL(t *testing.T) {
	u, err := buildExecURL("https://api.upuai.com.br", "env1", "svc1", "worker",
		[]string{"bin/rails", "console"}, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Scheme != "wss" {
		t.Errorf("scheme = %q, want wss (https→wss)", u.Scheme)
	}
	if got, want := u.Path, "/environments/env1/services/svc1/instance/exec"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	q := u.Query()
	if got := q["command"]; !reflect.DeepEqual(got, []string{"bin/rails", "console"}) {
		t.Errorf("command = %#v, want [bin/rails console]", got)
	}
	if q.Get("process") != "worker" {
		t.Errorf("process = %q, want worker", q.Get("process"))
	}
	// tty/stdin são sempre explícitos no CLI novo — é o que conserta o pipe.
	if q.Get("tty") != "false" {
		t.Errorf("tty = %q, want false", q.Get("tty"))
	}
	if q.Get("stdin") != "true" {
		t.Errorf("stdin = %q, want true", q.Get("stdin"))
	}

	// TTY true, sem stdin, sem process.
	u2, err := buildExecURL("http://localhost:8300", "e", "s", "", []string{"sh"}, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u2.Scheme != "ws" {
		t.Errorf("scheme = %q, want ws (http→ws)", u2.Scheme)
	}
	if u2.Query().Get("tty") != "true" {
		t.Errorf("tty = %q, want true", u2.Query().Get("tty"))
	}
	if u2.Query().Get("stdin") != "false" {
		t.Errorf("stdin = %q, want false", u2.Query().Get("stdin"))
	}
	if u2.Query().Has("process") {
		t.Errorf("process should be absent, got %q", u2.Query().Get("process"))
	}
}
