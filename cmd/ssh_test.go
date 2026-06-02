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
	tests := []struct {
		name        string
		args        []string
		wantSvcRef  string
		wantProcess string
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
			name:     "help short",
			args:     []string{"-h"},
			wantHelp: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetLeadingFlagGlobals()
			ref, process, cmd, help, err := parseSSHArgs(tc.args)
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
	if _, _, _, _, err := parseSSHArgs([]string{"-s"}); err == nil {
		t.Fatal("expected error for -s without a value")
	}
	if _, _, _, _, err := parseSSHArgs([]string{"--process"}); err == nil {
		t.Fatal("expected error for --process without a value")
	}
}
