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
			name:     "help short",
			args:     []string{"-h"},
			wantHelp: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetLeadingFlagGlobals()
			ref, process, ttyOverride, cmd, help, err := parseSSHArgs(tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if help != tc.wantHelp {
				t.Fatalf("help = %v, want %v", help, tc.wantHelp)
			}
			if tc.wantHelp {
				return
			}
			if ref != tc.wantSvcRef {
				t.Errorf("serviceRef = %q, want %q", ref, tc.wantSvcRef)
			}
			if process != tc.wantProcess {
				t.Errorf("process = %q, want %q", process, tc.wantProcess)
			}
			if !reflect.DeepEqual(ttyOverride, tc.wantTTY) {
				t.Errorf("ttyOverride = %v, want %v", ttyOverride, tc.wantTTY)
			}
			if !reflect.DeepEqual(cmd, tc.wantCommand) {
				t.Errorf("command = %#v, want %#v", cmd, tc.wantCommand)
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
	if _, _, _, _, _, err := parseSSHArgs([]string{"-s"}); err == nil {
		t.Fatal("expected error for -s without a value")
	}
	if _, _, _, _, _, err := parseSSHArgs([]string{"--process"}); err == nil {
		t.Fatal("expected error for --process without a value")
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
