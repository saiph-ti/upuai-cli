package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/skillinstall"
	"github.com/upuai-cloud/cli/internal/ui"
	"github.com/upuai-cloud/cli/skills"
)

var (
	flagSkillGlobal   bool
	flagSkillClaudeMd bool
	flagSkillForce    bool
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage the Upuai AI agent skill",
	Long: `Manage the Upuai agent skill for AI coding tools (Claude Code).

The skill teaches an agent how to deploy and operate this project with the upuai
CLI, so a fresh Claude Code session already knows what Upuai is. The CLI installs
it automatically the first time you run a command in a linked project; these
subcommands let you install it explicitly, target ~/.claude globally, or inspect
what's installed.`,
}

var skillInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install or refresh the Upuai skill in this project",
	Long: `Write the Upuai skill to .claude/skills/upuai/SKILL.md (or ~/.claude with
--global) so AI agents know how to deploy this project.

Idempotent: a file the CLI already wrote and you left untouched is refreshed only
when a newer skill ships. A hand-edited file (or one installed via 'npx skills
add') is left alone unless you pass --force.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			action skillinstall.Action
			err    error
			target string
		)
		if flagSkillGlobal {
			action, err = skillinstall.EnsureGlobal(flagSkillForce)
			target = filepath.Join("~", ".claude", "skills", skills.SkillName, "SKILL.md")
		} else {
			root := projectRootOrCwd()
			action, err = skillinstall.EnsureProject(root, flagSkillForce)
			target = filepath.Join(root, ".claude", "skills", skills.SkillName, "SKILL.md")
		}
		if err != nil {
			return fmt.Errorf("failed to install skill: %w", err)
		}
		reportSkillAction(action, target)

		if flagSkillClaudeMd {
			if flagSkillGlobal {
				ui.PrintWarning("--claude-md has no effect with --global (CLAUDE.md is project-scoped) — skipped")
			} else {
				root := projectRootOrCwd()
				cmAction, cmErr := skillinstall.WriteClaudeMdBlock(root)
				if cmErr != nil {
					return fmt.Errorf("failed to update CLAUDE.md: %w", cmErr)
				}
				switch cmAction {
				case skillinstall.ActInstalled, skillinstall.ActUpdated:
					ui.PrintSuccess("Added an Upuai pointer block to CLAUDE.md")
				case skillinstall.ActUpToDate:
					ui.PrintInfo("CLAUDE.md Upuai block already up to date")
				}
			}
		}
		return nil
	},
}

var skillStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show installed vs. bundled skill version and state",
	RunE: func(cmd *cobra.Command, args []string) error {
		proj := skillinstall.ProjectStatus(projectRootOrCwd())
		glob := skillinstall.GlobalStatus()

		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(map[string]interface{}{
				"bundledVersion": skills.Version(),
				"project":        proj,
				"global":         glob,
			})
			return nil
		}

		ui.PrintKeyValue("Bundled version", skills.Version())
		fmt.Println()
		printSkillStatus("Project", proj)
		printSkillStatus("Global", glob)
		return nil
	},
}

func printSkillStatus(scope string, s skillinstall.Status) {
	installed := s.InstalledVersion
	if installed == "" {
		installed = "—"
	}
	ui.PrintKeyValue(
		scope+" state", s.State,
		scope+" version", installed,
		scope+" path", s.Path,
	)
	fmt.Println()
}

// reportSkillAction narrates an explicit `upuai skill install` outcome (the
// auto-install path in skillinstall.MaybeEnsure stays quiet unless it writes).
func reportSkillAction(a skillinstall.Action, target string) {
	switch a {
	case skillinstall.ActInstalled:
		ui.PrintSuccess("Skill installed → " + target)
		ui.PrintInfo("New Claude Code sessions in this project can now deploy. Commit it to share with your team.")
	case skillinstall.ActUpdated:
		ui.PrintSuccess(fmt.Sprintf("Skill updated to v%s → %s", skills.Version(), target))
	case skillinstall.ActUpToDate:
		ui.PrintInfo(fmt.Sprintf("Skill already up to date (v%s) → %s", skills.Version(), target))
	case skillinstall.ActUserEdited:
		ui.PrintWarning(fmt.Sprintf("Skill was edited locally — left untouched. Re-run with --force to replace it with v%s.", skills.Version()))
	case skillinstall.ActUserManaged:
		ui.PrintWarning("An unmanaged skill file exists (e.g. from 'npx skills add') — left untouched. Re-run with --force to replace it.")
	}
}

// projectRootOrCwd anchors project-level writes to the directory that owns the
// linked project, falling back to the CWD when not inside one (so `upuai skill
// install` works before `upuai init`).
func projectRootOrCwd() string {
	if root, ok := config.ProjectRoot(); ok {
		return root
	}
	return "."
}

func init() {
	skillInstallCmd.Flags().BoolVar(&flagSkillGlobal, "global", false, "Install to ~/.claude (all projects on this machine) instead of this project")
	skillInstallCmd.Flags().BoolVar(&flagSkillClaudeMd, "claude-md", false, "Also add a managed Upuai pointer block to CLAUDE.md")
	skillInstallCmd.Flags().BoolVar(&flagSkillForce, "force", false, "Overwrite even a hand-edited or npx-installed skill file")

	skillCmd.AddCommand(skillInstallCmd)
	skillCmd.AddCommand(skillStatusCmd)
	rootCmd.AddCommand(skillCmd)
}
