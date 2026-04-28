package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/ui"
	"github.com/upuai-cloud/cli/pkg/version"
)

const githubReleasesURL = "https://api.github.com/repos/upuai-cloud/cli/releases/latest"

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade the CLI to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		ui.PrintInfo(fmt.Sprintf("Current version: %s", version.Short()))

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

		// Find matching asset
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
			return fmt.Errorf("no binary found for %s/%s — download manually from GitHub", osName, archName)
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

		// Replace binary
		if err := os.WriteFile(execPath, data, 0755); err != nil {
			return fmt.Errorf("failed to update binary: %w", err)
		}

		ui.PrintSuccess(fmt.Sprintf("Upgraded to v%s", latestVersion))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}
