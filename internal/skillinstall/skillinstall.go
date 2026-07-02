// Package skillinstall writes the Upuai agent skill into a project's
// `.claude/skills/upuai/` so any future Claude Code session there already knows
// how to deploy — without the user running `npx skills add` by hand.
//
// The install is idempotent and self-healing. It runs after every command in a
// linked project (MaybeEnsure, wired from cmd/root.go, mirroring updatecheck),
// so projects that predate this feature get the skill the first time the user
// runs any `upuai` command in them ("backfill"). The staleness model is
// content-hash based (see package skills): we only overwrite a file we wrote and
// the user left untouched; a hand-edited file, or one installed verbatim by `npx
// skills add`, is treated as user-managed and never clobbered.
package skillinstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/skills"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644 // committable doc, not a secret — mirrors a normal repo file
)

// skillRelPath is the project-relative location of the installed skill.
var skillRelPath = filepath.Join(".claude", "skills", skills.SkillName, "SKILL.md")

// Action reports what Ensure did (or why it declined).
type Action int

const (
	ActNone        Action = iota
	ActInstalled          // file did not exist → written
	ActUpdated            // we owned it, a newer skill shipped → rewritten
	ActUpToDate           // we owned it, already current → no-op
	ActUserEdited         // hand-edited since we wrote it → left untouched
	ActUserManaged        // no managed marker (e.g. `npx skills add`) → left untouched
)

// skipCommands never trigger the auto-ensure. Either there is no project context
// (login/logout), the user is explicitly managing the skill (skill), or the
// surrounding output is wrong (version/upgrade/completion/help).
var skipCommands = map[string]bool{
	"version":    true,
	"upgrade":    true,
	"completion": true,
	"help":       true,
	"login":      true,
	"logout":     true,
	"skill":      true,
}

// MaybeEnsure installs/refreshes the skill for the linked project after a
// command runs. It is silent unless it actually wrote the file, and never
// returns an error — a skill nudge must never break the user's workflow.
func MaybeEnsure(commandName string) {
	if optedOut() || skipCommands[commandName] {
		return
	}
	root, ok := config.ProjectRoot()
	if !ok {
		return // not inside a linked project
	}
	action, err := EnsureProject(root, false)
	if err != nil {
		return
	}
	switch action {
	case ActInstalled:
		fmt.Fprintf(os.Stderr,
			"\n\033[32m✓ Skill Upuai instalada\033[0m em %s — sessões novas do Claude Code aqui já sabem deployar. Commite pra compartilhar com o time.\n",
			skillRelPath)
	case ActUpdated:
		fmt.Fprintf(os.Stderr,
			"\n\033[32m✓ Skill Upuai atualizada para v%s\033[0m em %s.\n",
			skills.Version(), skillRelPath)
	}
	// ActUpToDate / ActUserEdited / ActUserManaged → silent. `upuai skill status`
	// surfaces those states on demand instead of nagging on every command.
}

// EnsureProject writes/refreshes the skill at <root>/.claude/skills/upuai/SKILL.md.
func EnsureProject(root string, force bool) (Action, error) {
	return ensureAt(filepath.Join(root, skillRelPath), force)
}

// EnsureGlobal writes/refreshes the skill at ~/.claude/skills/upuai/SKILL.md,
// applying to every project on the machine.
func EnsureGlobal(force bool) (Action, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ActNone, err
	}
	return ensureAt(filepath.Join(home, skillRelPath), force)
}

func ensureAt(path string, force bool) (Action, error) {
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ActInstalled, writeSkill(path)
	}
	if err != nil {
		return ActNone, err
	}
	if force {
		return ActUpdated, writeSkill(path)
	}
	switch classify(string(existing)) {
	case stCurrent:
		return ActUpToDate, nil
	case stUserEdited:
		return ActUserEdited, nil
	case stUserManaged:
		return ActUserManaged, nil
	default: // stStale
		return ActUpdated, writeSkill(path)
	}
}

type state int

const (
	stCurrent state = iota
	stStale
	stUserEdited
	stUserManaged
)

// classify decides how an already-present file relates to what we'd write.
func classify(installed string) state {
	_, markerSHA, ok := skills.ParseMarker(installed)
	if !ok {
		return stUserManaged // no marker → not ours to overwrite
	}
	if skills.SHA256Hex(skills.StripMarker(installed)) != markerSHA {
		return stUserEdited // content diverged from what the marker records
	}
	if markerSHA != skills.BundledSHA() {
		return stStale // ours, untouched, but a newer skill shipped
	}
	return stCurrent
}

// writeSkill atomically writes the Claude-Code-rendered skill (temp + rename) so
// a concurrent command never observes a half-written SKILL.md.
func writeSkill(path string) error {
	return atomicWriteString(path, skills.RenderForClaudeCode(), filePerm)
}

func optedOut() bool {
	if os.Getenv("UPUAI_SKIP_SKILL_INSTALL") == "1" {
		return true
	}
	return !config.SkillAutoInstallEnabled()
}

// Status describes the installed skill relative to the bundled one, for
// `upuai skill status`.
type Status struct {
	Path             string
	Installed        bool
	State            string // "up-to-date" | "stale" | "user-edited" | "user-managed" | "not-installed"
	InstalledVersion string
	BundledVersion   string
}

// ProjectStatus inspects <root>/.claude/skills/upuai/SKILL.md.
func ProjectStatus(root string) Status {
	return statusAt(filepath.Join(root, skillRelPath))
}

// GlobalStatus inspects ~/.claude/skills/upuai/SKILL.md.
func GlobalStatus() Status {
	home, err := os.UserHomeDir()
	if err != nil {
		return Status{State: "not-installed", BundledVersion: skills.Version()}
	}
	return statusAt(filepath.Join(home, skillRelPath))
}

func statusAt(path string) Status {
	s := Status{Path: path, BundledVersion: skills.Version()}
	data, err := os.ReadFile(path)
	if err != nil {
		s.State = "not-installed"
		return s
	}
	s.Installed = true
	ver, _, _ := skills.ParseMarker(string(data))
	s.InstalledVersion = ver
	switch classify(string(data)) {
	case stCurrent:
		s.State = "up-to-date"
	case stStale:
		s.State = "stale"
	case stUserEdited:
		s.State = "user-edited"
	case stUserManaged:
		s.State = "user-managed"
	}
	return s
}
