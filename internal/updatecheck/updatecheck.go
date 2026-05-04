// Package updatecheck nudges the user when a newer CLI release is on GitHub.
//
// The check runs at most once every 24h (cached at ~/.upuai/last-update-check.json),
// after the user's command finishes, and prints to stderr so it never mixes with
// stdout (a `upuai status -o json | jq` pipeline keeps working).
//
// Disable with UPUAI_DISABLE_UPDATE_CHECK=1 (CI/agent contexts).
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/upuai-cloud/cli/pkg/version"
)

const (
	releasesURL = "https://api.github.com/repos/saiph-ti/upuai-cli/releases/latest"
	cacheTTL    = 24 * time.Hour
	cacheFile   = "last-update-check.json"
	upuaiDir    = ".upuai"
	httpTimeout = 3 * time.Second
)

// SkipCommands are excluded from the post-run notification. Either because the
// user already saw version info (version, upgrade) or because the surrounding
// context is wrong (completion shell scripts, help screens).
var SkipCommands = map[string]bool{
	"version":    true,
	"upgrade":    true,
	"completion": true,
	"help":       true,
}

type cache struct {
	LastCheckedAt int64  `json:"lastCheckedAt"`
	LatestVersion string `json:"latestVersion"`
}

type release struct {
	TagName string `json:"tag_name"`
}

// MaybeNotify checks GitHub for a newer release (respecting cache + opt-out)
// and prints a one-line nudge to stderr if the user is behind. Errors are
// swallowed silently — an update nudge must never break the user's workflow.
func MaybeNotify(commandName string) {
	if os.Getenv("UPUAI_DISABLE_UPDATE_CHECK") == "1" {
		return
	}
	if SkipCommands[commandName] {
		return
	}

	current := strings.TrimPrefix(version.Short(), "v")
	if current == "" || current == "dev" || current == "unknown" {
		return // local/snapshot build — irrelevant
	}

	latest, fromCache := readCache()
	if !fromCache {
		var err error
		latest, err = fetchLatest()
		if err != nil {
			return
		}
		writeCache(latest)
	}
	if latest == "" {
		return
	}

	if newer(latest, current) {
		fmt.Fprintf(os.Stderr,
			"\n\033[33m✨ upuai v%s available\033[0m (you have v%s) — run \033[1mupuai upgrade\033[0m to update.\n",
			latest, current,
		)
	}
}

func cachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, upuaiDir, cacheFile), nil
}

// readCache returns (latestVersion, fromCache). When the cache is fresh, it is
// authoritative and the caller skips the network. When stale or absent, the
// caller fetches and rewrites.
func readCache() (string, bool) {
	path, err := cachePath()
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var c cache
	if err := json.Unmarshal(data, &c); err != nil {
		return "", false
	}
	if time.Since(time.Unix(c.LastCheckedAt, 0)) > cacheTTL {
		return c.LatestVersion, false
	}
	return c.LatestVersion, true
}

func writeCache(latest string) {
	path, err := cachePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(cache{LastCheckedAt: time.Now().Unix(), LatestVersion: latest})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func fetchLatest() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "upuai-cli/"+version.Short())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases: HTTP %d", resp.StatusCode)
	}
	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	return strings.TrimPrefix(r.TagName, "v"), nil
}

// newer reports whether `latest` is strictly newer than `current` using lexical
// semver comparison on numeric components. Both inputs must be of the form
// "X.Y.Z[-suffix]" with X/Y/Z integers; suffixes are ignored.
func newer(latest, current string) bool {
	la := splitSemver(latest)
	cu := splitSemver(current)
	for i := 0; i < 3; i++ {
		if la[i] != cu[i] {
			return la[i] > cu[i]
		}
	}
	return false
}

func splitSemver(v string) [3]int {
	v = strings.SplitN(v, "-", 2)[0]
	parts := strings.SplitN(v, ".", 3)
	out := [3]int{}
	for i := 0; i < 3 && i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}
