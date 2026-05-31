package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/archive"
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

// upgradeCommands returns the ordered sequence of commands to upgrade via the
// package manager. The refresh step (`brew update` / `scoop update`) is
// MANDATORY and must run first: `brew upgrade upuai` alone never sees a newly
// released version because Homebrew only re-reads the tap clone on `brew
// update` — without it the upgrade reports "already installed" forever. Scoop
// is the same: `scoop update <app>` upgrades to the version in the cached
// manifest, while `scoop update` (no arg) refreshes the bucket manifests first.
func (m installMethod) upgradeCommands() (cmds [][]string, ok bool) {
	switch m {
	case installBrew:
		return [][]string{{"brew", "update"}, {"brew", "upgrade", "upuai"}}, true
	case installScoop:
		return [][]string{{"scoop", "update"}, {"scoop", "update", "upuai"}}, true
	default:
		return nil, false
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
			if cmds, ok := method.upgradeCommands(); ok {
				for _, cmdArgs := range cmds {
					ui.PrintInfo(fmt.Sprintf("Would run:       %s", strings.Join(cmdArgs, " ")))
				}
			} else {
				ui.PrintInfo("Would run:       direct download from GitHub releases")
			}
			return nil
		}

		ui.PrintInfo(fmt.Sprintf("Current version: %s", version.Short()))
		ui.PrintInfo(fmt.Sprintf("Install method:  %s", method))

		// Delegate to the package manager when one is in charge — overwriting
		// the binary directly under a managed prefix breaks future upgrades.
		// Runs the full sequence (refresh index, then upgrade) in order.
		if cmds, ok := method.upgradeCommands(); ok {
			fmt.Println()
			for _, cmdArgs := range cmds {
				ui.PrintInfo(fmt.Sprintf("Running: %s", strings.Join(cmdArgs, " ")))
				c := exec.Command(cmdArgs[0], cmdArgs[1:]...)
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				c.Stdin = os.Stdin
				if err := c.Run(); err != nil {
					return fmt.Errorf("%s upgrade failed at `%s`: %w", method, strings.Join(cmdArgs, " "), err)
				}
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

	// Assets do goreleaser são upuai_<ver>_<os>_<arch>.<ext> (arch amd64→x86_64,
	// .zip só no Windows). Casamos por SUFIXO — imune ao segmento de versão no meio
	// do nome e sem colidir com checksums.txt.
	suffix := assetSuffix(runtime.GOOS, runtime.GOARCH)
	var asset githubAsset
	for _, a := range release.Assets {
		if strings.HasSuffix(a.Name, suffix) {
			asset = a
			break
		}
	}
	if asset.BrowserDownloadURL == "" {
		return fmt.Errorf("no release asset matching *%s — download manually from https://github.com/saiph-ti/upuai-cli/releases/latest", suffix)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}
	// Segue symlink (ex.: /usr/local/bin/upuai → caminho real) pra trocar o arquivo certo.
	if resolved, lerr := filepath.EvalSymlinks(execPath); lerr == nil {
		execPath = resolved
	}

	var data []byte
	err = ui.RunWithSpinner(fmt.Sprintf("Downloading v%s...", latestVersion), func() error {
		client := &http.Client{Timeout: 120 * time.Second}
		resp, rerr := client.Get(asset.BrowserDownloadURL)
		if rerr != nil {
			return rerr
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
		}
		data, rerr = io.ReadAll(resp.Body)
		return rerr
	})
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}

	// O asset é um archive — extrai o binário antes de trocar (gravar o .tar.gz cru
	// por cima do executável corromperia o CLI).
	bin, err := archive.ExtractBinary(data, asset.Name, binaryName(runtime.GOOS))
	if err != nil {
		return fmt.Errorf("failed to extract binary from %s: %w", asset.Name, err)
	}

	if err := replaceExecutable(execPath, bin); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			// Path gerenciado por root (ex.: /usr/local/bin) — não dá pra trocar sem sudo.
			// Em vez de falhar, mostra o comando manual exato (sem nunca corromper o binário).
			printManualUpgrade(asset, latestVersion, execPath)
			return nil
		}
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	ui.PrintSuccess(fmt.Sprintf("Upgraded to v%s", latestVersion))
	return nil
}

// assetSuffix devolve o sufixo do nome do asset do goreleaser para um GOOS/GOARCH —
// linux/amd64 → "_linux_x86_64.tar.gz", darwin/arm64 → "_darwin_arm64.tar.gz",
// windows/amd64 → "_windows_x86_64.zip". Espelha o name_template do .goreleaser.yaml
// (amd64→x86_64; .zip só no Windows). Mantido puro pra ser testável sem rede.
func assetSuffix(goos, goarch string) string {
	arch := goarch
	if arch == "amd64" {
		arch = "x86_64"
	}
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("_%s_%s.%s", goos, arch, ext)
}

// binaryName é o nome do executável dentro do archive.
func binaryName(goos string) string {
	if goos == "windows" {
		return "upuai.exe"
	}
	return "upuai"
}

// replaceExecutable troca o binário em execPath de forma atômica: grava num temp no
// MESMO diretório (rename atômico exige mesmo filesystem), chmod 0755 e renomeia por
// cima. No Windows não dá pra sobrescrever um .exe em uso, então renomeia o atual pra
// .old antes. Devolve fs.ErrPermission (não-embrulhado) quando o diretório não é
// gravável, pra o caller cair no fallback manual.
func replaceExecutable(execPath string, data []byte) error {
	dir := filepath.Dir(execPath)
	tmp, err := os.CreateTemp(dir, ".upuai-upgrade-*")
	if err != nil {
		return err // ErrPermission quando o dir é de root
	}
	tmpPath := tmp.Name()
	defer func() {
		if _, statErr := os.Stat(tmpPath); statErr == nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Rename(execPath, execPath+".old")
	}
	return os.Rename(tmpPath, execPath)
}

// printManualUpgrade imprime o comando exato pra atualizar quando a troca in-process
// não é possível (path de root). Usa o asset/tag corretos já resolvidos.
func printManualUpgrade(asset githubAsset, version, execPath string) {
	ui.PrintWarning(fmt.Sprintf("Cannot replace %s without elevated permissions.", execPath))
	fmt.Println()
	ui.PrintInfo(fmt.Sprintf("Upgrade to v%s manually:", version))
	fmt.Println()
	if strings.HasSuffix(asset.Name, ".zip") {
		fmt.Printf("  curl -sSfL %s -o upuai.zip && unzip -o upuai.zip upuai.exe\n", asset.BrowserDownloadURL)
		return
	}
	fmt.Printf("  curl -sSfL %s | tar -xz upuai\n", asset.BrowserDownloadURL)
	fmt.Printf("  sudo mv upuai %s\n", execPath)
}

func init() {
	upgradeCmd.Flags().BoolVar(&flagUpgradeCheck, "check", false, "Show install method and the upgrade command without running it")
	rootCmd.AddCommand(upgradeCmd)
}
