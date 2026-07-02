package skillinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/upuai-cloud/cli/skills"
)

func skillPath(root string) string {
	return filepath.Join(root, ".claude", "skills", skills.SkillName, "SKILL.md")
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// craftOld builds a self-consistent installed file for an *older* skill version:
// a valid managed marker whose sha matches its own (different) content, so the
// installer sees "ours, untouched, but stale".
func craftOld() (file string) {
	frontmatter := "---\nname: \"upuai\"\ndescription: \"old\"\n---\n"
	oldBody := "OLD BODY\n"
	stripped := frontmatter + "\n" + oldBody
	oldSHA := skills.SHA256Hex(stripped)
	marker := "<!-- upuai skill v0.0.1 · sha256:" + oldSHA + " · managed by the upuai CLI -->\n"
	return frontmatter + marker + "\n" + oldBody
}

func TestEnsureInstallsWhenAbsent(t *testing.T) {
	root := t.TempDir()
	action, err := EnsureProject(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActInstalled {
		t.Fatalf("action = %v, want ActInstalled", action)
	}
	got := readFile(t, skillPath(root))
	if got != skills.RenderForClaudeCode() {
		t.Error("installed file does not match RenderForClaudeCode()")
	}
}

func TestEnsureWritesAtRootNotCwd(t *testing.T) {
	root := t.TempDir()
	if _, err := EnsureProject(root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(skillPath(root)); err != nil {
		t.Fatalf("skill not written under project root: %v", err)
	}
}

func TestEnsureNoOpWhenCurrent(t *testing.T) {
	root := t.TempDir()
	if _, err := EnsureProject(root, false); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, skillPath(root))

	action, err := EnsureProject(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActUpToDate {
		t.Fatalf("action = %v, want ActUpToDate", action)
	}
	if after := readFile(t, skillPath(root)); after != before {
		t.Error("up-to-date file was rewritten")
	}
}

func TestEnsureUpdatesWhenStale(t *testing.T) {
	root := t.TempDir()
	path := skillPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(craftOld()), 0o644); err != nil {
		t.Fatal(err)
	}

	action, err := EnsureProject(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActUpdated {
		t.Fatalf("action = %v, want ActUpdated", action)
	}
	if got := readFile(t, path); got != skills.RenderForClaudeCode() {
		t.Error("stale file was not refreshed to the bundled skill")
	}
}

func TestEnsureLeavesHandEditedAlone(t *testing.T) {
	root := t.TempDir()
	path := skillPath(root)
	if _, err := EnsureProject(root, false); err != nil {
		t.Fatal(err)
	}
	edited := readFile(t, path) + "\n<!-- my local note -->\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	action, err := EnsureProject(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActUserEdited {
		t.Fatalf("action = %v, want ActUserEdited", action)
	}
	if got := readFile(t, path); got != edited {
		t.Error("hand-edited file was clobbered")
	}
}

func TestEnsureLeavesUnmarkedAlone(t *testing.T) {
	root := t.TempDir()
	path := skillPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	unmarked := "---\nname: upuai\ndescription: installed by npx\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(unmarked), 0o644); err != nil {
		t.Fatal(err)
	}

	action, err := EnsureProject(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActUserManaged {
		t.Fatalf("action = %v, want ActUserManaged", action)
	}
	if got := readFile(t, path); got != unmarked {
		t.Error("unmanaged (npx-style) file was clobbered")
	}
}

func TestForceOverwritesUserManaged(t *testing.T) {
	root := t.TempDir()
	path := skillPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hand written, no marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	action, err := EnsureProject(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActUpdated {
		t.Fatalf("action = %v, want ActUpdated", action)
	}
	if got := readFile(t, path); got != skills.RenderForClaudeCode() {
		t.Error("--force did not overwrite with the bundled skill")
	}
}

func TestClaudeMdBlockIdempotent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# My Project\n\nStuff.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a1, err := WriteClaudeMdBlock(root)
	if err != nil {
		t.Fatal(err)
	}
	if a1 != ActInstalled {
		t.Fatalf("first write action = %v, want ActInstalled", a1)
	}
	after1 := readFile(t, filepath.Join(root, "CLAUDE.md"))

	a2, err := WriteClaudeMdBlock(root)
	if err != nil {
		t.Fatal(err)
	}
	if a2 != ActUpToDate {
		t.Fatalf("second write action = %v, want ActUpToDate", a2)
	}
	if after2 := readFile(t, filepath.Join(root, "CLAUDE.md")); after2 != after1 {
		t.Error("re-writing the CLAUDE.md block was not idempotent")
	}
	if !strings.Contains(after1, "# My Project") {
		t.Error("existing CLAUDE.md content was lost")
	}
}

func TestProjectStatus(t *testing.T) {
	root := t.TempDir()
	if s := ProjectStatus(root); s.State != "not-installed" {
		t.Fatalf("fresh dir state = %q, want not-installed", s.State)
	}
	if _, err := EnsureProject(root, false); err != nil {
		t.Fatal(err)
	}
	s := ProjectStatus(root)
	if s.State != "up-to-date" {
		t.Errorf("post-install state = %q, want up-to-date", s.State)
	}
	if s.InstalledVersion != skills.Version() {
		t.Errorf("installed version = %q, want %q", s.InstalledVersion, skills.Version())
	}
}
