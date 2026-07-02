// Package skills embeds the canonical Upuai agent skill (skills/upuai/SKILL.md)
// so the CLI can install it into a user's project without a network round-trip.
//
// The source file is authored in the Mintlify "Agent Skills" open format — the
// same file `npx skills add saiph-ti/upuai-cli --skill upuai` copies verbatim.
// Claude Code, however, reads a slightly different frontmatter schema
// (https://code.claude.com/docs/en/skills): it honours `when_to_use` (underscore,
// not the Mintlify `when-to-use`) and ignores `version`/`homepage`. RenderForClaudeCode
// transforms the frontmatter into exactly what Claude Code reads, and stamps a
// managed marker carrying the version + a content hash so the installer can tell
// "up to date" from "stale" from "hand-edited by the user".
package skills

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

//go:embed upuai/SKILL.md
var mintlifySkill string

// SkillName is the directory/skill name used under `.claude/skills/<name>/`.
const SkillName = "upuai"

type sotMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	WhenToUse   string `yaml:"when-to-use"`
	Version     string `yaml:"version"`
}

var frontmatterRe = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---\r?\n(.*)\z`)

// markerRe matches the single managed-marker comment line RenderForClaudeCode
// inserts right after the frontmatter. It is the anchor the installer strips to
// recover the canonical content for hashing.
var markerRe = regexp.MustCompile(`(?m)^<!-- upuai skill .*?-->\r?\n`)

var markerShaRe = regexp.MustCompile(`sha256:([0-9a-f]{64})`)
var markerVerRe = regexp.MustCompile(`upuai skill v([^\s]+) `)

// parsed holds the SOT metadata + body, computed once.
var (
	meta sotMeta
	body string
)

func init() {
	m := frontmatterRe.FindStringSubmatch(mintlifySkill)
	if m == nil {
		// The embedded SOT is ours and validated in tests; a shape we can't parse
		// is a build-time authoring error, surfaced loudly rather than silently
		// shipping a skill with no metadata.
		panic("skills: embedded upuai/SKILL.md has no parseable frontmatter")
	}
	if err := yaml.Unmarshal([]byte(m[1]), &meta); err != nil {
		panic(fmt.Sprintf("skills: embedded upuai/SKILL.md frontmatter is not valid YAML: %v", err))
	}
	body = strings.TrimLeft(m[2], "\r\n")
}

// Version is the SOT skill version (frontmatter `version:`), shown to humans.
func Version() string { return meta.Version }

// frontmatterBlock emits the Claude-Code-native frontmatter: only fields Claude
// Code actually reads. Values are hand-emitted as double-quoted YAML scalars so
// long single-line descriptions never get wrapped or misparsed (the two strings
// are ours and contain no `"`/`\`, but we escape defensively).
func frontmatterBlock() string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + yamlDoubleQuoted(meta.Name) + "\n")
	b.WriteString("description: " + yamlDoubleQuoted(meta.Description) + "\n")
	if meta.WhenToUse != "" {
		b.WriteString("when_to_use: " + yamlDoubleQuoted(meta.WhenToUse) + "\n")
	}
	b.WriteString("---\n")
	return b.String()
}

// canonicalContent is the marker-free rendering: frontmatter + body. It is what
// BundledSHA hashes and what StripMarker recovers from an installed file, so the
// two always compare apples to apples.
func canonicalContent() string {
	return frontmatterBlock() + "\n" + body
}

// BundledSHA is the sha256 (hex) of the canonical content this binary would emit.
func BundledSHA() string {
	sum := sha256.Sum256([]byte(canonicalContent()))
	return hex.EncodeToString(sum[:])
}

// RenderForClaudeCode is the full file written to disk: canonical content with
// the managed marker inserted between the frontmatter and the body.
func RenderForClaudeCode() string {
	marker := fmt.Sprintf(
		"<!-- upuai skill v%s · sha256:%s · managed by the upuai CLI (upstream: saiph-ti/upuai-cli skills/upuai). Do not edit here — run `upuai skill install --force` to refresh. -->\n",
		meta.Version, BundledSHA(),
	)
	return frontmatterBlock() + marker + "\n" + body
}

// StripMarker removes the managed-marker line, recovering canonical content for
// hashing. On content we wrote and the user left untouched, StripMarker(installed)
// == canonicalContent() and thus hashes to the embedded marker's sha.
func StripMarker(content string) string {
	return markerRe.ReplaceAllString(content, "")
}

// ParseMarker extracts (version, sha) from an installed file's managed marker.
// ok is false when the file has no marker (e.g. installed verbatim via `npx
// skills add`, or authored by hand) — the installer treats those as user-managed
// and never clobbers them.
func ParseMarker(content string) (version, sha string, ok bool) {
	line := markerRe.FindString(content)
	if line == "" {
		return "", "", false
	}
	s := markerShaRe.FindStringSubmatch(line)
	v := markerVerRe.FindStringSubmatch(line)
	if s == nil {
		return "", "", false
	}
	sha = s[1]
	if v != nil {
		version = v[1]
	}
	return version, sha, true
}

// SHA256Hex is a small shared helper so callers hash exactly the way BundledSHA does.
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func yamlDoubleQuoted(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
