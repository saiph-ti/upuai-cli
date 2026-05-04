package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/ui"
	"github.com/upuai-cloud/cli/pkg/version"
)

// githubReleasesURL points at the public CLI repo (saiph-ti/upuai-cli).
// The Homebrew tap (saiph-ti/homebrew-upuai-cli) and Scoop bucket
// (saiph-ti/scoop-upuai-cli) are auto-updated by goreleaser on tag push.
const githubReleasesURL = "https://api.github.com/repos/saiph-ti/upuai-cli/releases/latest"

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// installMethod describes how the running binary was installed, which dictates
// the right way to upgrade it (delegating to brew/scoop instead of overwriting
// the binary directly — if we overwrite a brew-managed file, the next
// `brew upgrade` will silently restore the old version).
type installMethod int

const (
	installRaw installMethod = iota
	installBrew
	installScoop
)

func (m installMethod) String() string {
	switch m {
	case installBrew:
		return "Homebrew"
	case installScoop:
		return "Scoop"
	default:
		return "direct download"
	}
}

func (m installMethod) upgradeCommand() (program string, args []string, ok bool) {
	switch m {
	case installBrew:
		return "brew", []string{"upgrade", "upuai"}, true
	case installScoop:
		return "scoop", []string{"update", "upuai"}, true
	default:
		return "", nil, false
	}
}

func detectInstallMethod() installMethod {
	exe, err := os.Executable()
	if err != nil {
		return installRaw
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	lower := strings.ToLower(resolved)
	switch {
	case strings.Contains(lower, "/cellar/upuai/"),
		strings.Contains(lower, "/homebrew/"),
		strings.Contains(lower, "/linuxbrew/"):
		return installBrew
	case strings.Contains(lower, `\scoop\`),
		strings.Contains(lower, "/scoop/"):
		return installScoop
	default:
		return installRaw
	}
}

var (
	flagUpgradeCheck bool
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade the CLI to the latest version",
	Long: `Upgrade the CLI to the latest GitHub release.

Detects how the binary was installed (Homebrew, Scoop, or direct download)
and delegates to the right package manager — running 'brew upgrade upuai'
when installed via Homebrew, 'scoop update upuai' on Scoop, or downloading
the new binary directly otherwise.

Use --check to preview what would run without upgrading.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		method := detectInstallMethod()

		if flagUpgradeCheck {
			ui.PrintInfo(fmt.Sprintf("Current version: %s", version.Short()))
			ui.PrintInfo(fmt.Sprintf("Install method:  %s", method))
			if program, methodArgs, ok := method.upgradeCommand(); ok {
				ui.PrintInfo(fmt.Sprintf("Would run:       %s %s", program, strings.Join(methodArgs, " ")))
			} else {
				ui.PrintInfo("Would run:       direct download from GitHub releases")
			}
			return nil
		}

		ui.PrintInfo(fmt.Sprintf("Current version: %s", version.Short()))
		ui.PrintInfo(fmt.Sprintf("Install method:  %s", method))

		// Delegate to the package manager when one is in charge — overwriting
		// the binary directly under a managed prefix breaks future upgrades.
		if program, methodArgs, ok := method.upgradeCommand(); ok {
			fmt.Println()
			ui.PrintInfo(fmt.Sprintf("Running: %s %s", program, strings.Join(methodArgs, " ")))
			c := exec.Command(program, methodArgs...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			c.Stdin = os.Stdin
			if err := c.Run(); err != nil {
				return fmt.Errorf("%s upgrade failed: %w", method, err)
			}
			return nil
		}

		return runDirectUpgrade()
	},
}

func runDirectUpgrade() error {
	var release githubRelease
	err := ui.RunWithSpinner("Checking for updates...", func() error {
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(githubReleasesURL)
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to check for updates: HTTP %d", resp.StatusCode)
		}

		return json.NewDecoder(resp.Body).Decode(&release)
	})
	if err != nil {
		return err
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(version.Short(), "v")

	if latestVersion == currentVersion {
		ui.PrintSuccess("Already on the latest version")
		return nil
	}

	osName := runtime.GOOS
	archName := runtime.GOARCH
	assetName := fmt.Sprintf("upuai_%s_%s", osName, archName)

	var downloadURL string
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, assetName) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s/%s — download manually from https://github.com/saiph-ti/upuai-cli/releases/latest", osName, archName)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}

	var data []byte
	err = ui.RunWithSpinner(fmt.Sprintf("Downloading v%s...", latestVersion), func() error {
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Get(downloadURL)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
		}

		data, err = io.ReadAll(resp.Body)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}

	if err := os.WriteFile(execPath, data, 0755); err != nil {
		return fmt.Errorf("failed to update binary: %w", err)
	}

	ui.PrintSuccess(fmt.Sprintf("Upgraded to v%s", latestVersion))
	return nil
}

func init() {
	upgradeCmd.Flags().BoolVar(&flagUpgradeCheck, "check", false, "Show install method and the upgrade command without running it")
	rootCmd.AddCommand(upgradeCmd)
}
