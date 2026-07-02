package skillinstall

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The managed block gives an at-a-glance pointer in CLAUDE.md/AGENTS.md for
// sessions that read the top-level context but don't trigger the skill. It is
// opt-in (`upuai skill install --claude-md`) because it edits a file the user
// owns; the skill itself (own namespace) is what the auto-install writes.
const (
	claudeMdBegin = "<!-- upuai:begin -->"
	claudeMdEnd   = "<!-- upuai:end -->"
)

var claudeMdBlockRe = regexp.MustCompile(`(?s)<!-- upuai:begin -->.*?<!-- upuai:end -->\n?`)

func claudeMdBlock() string {
	return claudeMdBegin + "\n" +
		"## Deploy — Upuai Cloud\n\n" +
		"This project deploys to **Upuai Cloud** via the `upuai` CLI. The full workflow lives in the `upuai` skill (`.claude/skills/upuai/SKILL.md`, auto-loaded by Claude Code). Quick reference: `upuai deploy --wait` (git-connected service) or `upuai up` (local directory); `upuai status`, `upuai logs`, `upuai rollback` for ops.\n" +
		claudeMdEnd + "\n"
}

// WriteClaudeMdBlock adds (or refreshes in place) the managed Upuai block in
// <root>/CLAUDE.md, creating the file if absent. Idempotent: an existing block
// is replaced; otherwise the block is appended.
func WriteClaudeMdBlock(root string) (Action, error) {
	path := filepath.Join(root, "CLAUDE.md")
	block := claudeMdBlock()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ActInstalled, atomicWriteString(path, block, filePerm)
	}
	if err != nil {
		return ActNone, err
	}

	content := string(data)
	if loc := claudeMdBlockRe.FindString(content); loc != "" {
		if strings.TrimRight(loc, "\n") == strings.TrimRight(block, "\n") {
			return ActUpToDate, nil
		}
		updated := claudeMdBlockRe.ReplaceAllString(content, block)
		return ActUpdated, atomicWriteString(path, updated, filePerm)
	}

	// Append, guaranteeing a blank line before the block.
	sep := "\n"
	if !strings.HasSuffix(content, "\n") {
		sep = "\n\n"
	} else if !strings.HasSuffix(content, "\n\n") {
		sep = "\n"
	}
	return ActInstalled, atomicWriteString(path, content+sep+block, filePerm)
}

func atomicWriteString(path, content string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".upuai-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
