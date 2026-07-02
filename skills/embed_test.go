package skills

import (
	"strings"
	"testing"
)

func TestEmbedNonEmpty(t *testing.T) {
	if strings.TrimSpace(mintlifySkill) == "" {
		t.Fatal("embedded upuai/SKILL.md is empty")
	}
	if Version() == "" {
		t.Fatal("skill Version() is empty — frontmatter `version:` missing?")
	}
	if strings.TrimSpace(body) == "" {
		t.Fatal("skill body is empty after frontmatter split")
	}
}

// TestRenderForClaudeCode is the crux of the "transform, don't copy" decision:
// the emitted frontmatter must use Claude Code's field names and drop the
// Mintlify-only ones.
func TestRenderForClaudeCode(t *testing.T) {
	out := RenderForClaudeCode()

	fm := out[:strings.Index(out[4:], "---")+4] // frontmatter region (between the two ---)

	if !strings.Contains(fm, "when_to_use:") {
		t.Error("Claude Code frontmatter must carry when_to_use (underscore)")
	}
	if strings.Contains(fm, "when-to-use:") {
		t.Error("hyphenated when-to-use leaked into Claude Code frontmatter — it would be ignored")
	}
	if strings.Contains(fm, "version:") || strings.Contains(fm, "homepage:") {
		t.Error("version/homepage must not appear in Claude Code frontmatter (unrecognized fields)")
	}
	if !strings.Contains(fm, `name: "upuai"`) {
		t.Error("frontmatter missing name")
	}
	if !strings.Contains(fm, "description:") {
		t.Error("frontmatter missing description")
	}

	// Marker present, carrying the bundled sha + version.
	ver, sha, ok := ParseMarker(out)
	if !ok {
		t.Fatal("rendered skill has no parseable managed marker")
	}
	if sha != BundledSHA() {
		t.Errorf("marker sha %q != BundledSHA %q", sha, BundledSHA())
	}
	if ver != Version() {
		t.Errorf("marker version %q != Version %q", ver, Version())
	}
}

// TestCanonicalRoundtrip guarantees the installer's staleness check is coherent:
// stripping the marker from what we write and re-hashing must reproduce BundledSHA.
func TestCanonicalRoundtrip(t *testing.T) {
	out := RenderForClaudeCode()
	stripped := StripMarker(out)
	if got := SHA256Hex(stripped); got != BundledSHA() {
		t.Errorf("SHA256Hex(StripMarker(render)) = %q, want BundledSHA %q", got, BundledSHA())
	}
	if strings.Contains(stripped, "<!-- upuai skill ") {
		t.Error("StripMarker left the managed marker behind")
	}
}

func TestDescriptionBudget(t *testing.T) {
	// Claude Code caps description + when_to_use at 1536 chars in the listing.
	total := len(meta.Description) + len(meta.WhenToUse)
	if total > 1536 {
		t.Errorf("description+when_to_use = %d chars, exceeds Claude Code's 1536 cap", total)
	}
}
