package cmd

import (
	"reflect"
	"testing"
)

func TestParseRunArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantSvcRef  string
		wantCommand []string
		wantHelp    bool
		wantProject string
		wantEnv     string
	}{
		{
			// Same DisableFlagParsing class of bug as ssh: -p/-e were dropped.
			name:        "global -p/-e before -s and command",
			args:        []string{"-p", "adv-os", "-e", "production", "-s", "web", "--", "npm", "start"},
			wantSvcRef:  "web",
			wantCommand: []string{"npm", "start"},
			wantProject: "adv-os",
			wantEnv:     "production",
		},
		{
			name:        "remote command keeps its own flags",
			args:        []string{"-s", "api", "--", "rails", "runner", "-e", "production"},
			wantSvcRef:  "api",
			wantCommand: []string{"rails", "runner", "-e", "production"},
		},
		{
			name:        "no -- separator",
			args:        []string{"-p", "proj", "echo", "hi"},
			wantSvcRef:  "",
			wantCommand: []string{"echo", "hi"},
			wantProject: "proj",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetLeadingFlagGlobals()
			ref, cmd, help, err := parseRunArgs(tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if help != tc.wantHelp {
				t.Fatalf("help = %v, want %v", help, tc.wantHelp)
			}
			if ref != tc.wantSvcRef {
				t.Errorf("serviceRef = %q, want %q", ref, tc.wantSvcRef)
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
