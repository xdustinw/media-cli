// Package selfupdate replaces the running mc binary with a build from a GitHub
// release. It has no third-party dependencies.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub "owner/name" this CLI updates from.
const Repo = "xdustinw/media-cli"

// apiBase is the GitHub REST API root. It is a variable so tests can point the
// release lookups at a local server.
var apiBase = "https://api.github.com"

// Release is the subset of the GitHub release payload we use.
type Release struct {
	TagName    string  `json:"tag_name"`
	HTMLURL    string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset is one uploaded release file.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// AssetName returns the release asset filename for the current OS/arch, e.g.
// "mc-linux-amd64" or "mc-windows-amd64.exe".
func AssetName() string {
	name := fmt.Sprintf("mc-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// FindAsset returns the asset matching AssetName, or nil.
func (r Release) FindAsset() *Asset {
	want := AssetName()
	for i := range r.Assets {
		if r.Assets[i].Name == want {
			return &r.Assets[i]
		}
	}
	return nil
}

// Releases fetches the most recent releases for Repo (newest first, as GitHub
// orders them). Drafts are included as returned; callers filter them out.
func Releases(ctx context.Context) ([]Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=30", apiBase, Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}
	var rels []Release
	if err := json.Unmarshal(body, &rels); err != nil {
		return nil, fmt.Errorf("parsing releases response: %w", err)
	}
	return rels, nil
}

// LatestReleases returns the newest stable release and the newest pre-release
// for Repo. Either may be nil when none exists. Drafts and releases without a
// usable tag are ignored; "newest" is by version order (see Compare).
func LatestReleases(ctx context.Context) (stable, preview *Release, err error) {
	rels, err := Releases(ctx)
	if err != nil {
		return nil, nil, err
	}
	for i := range rels {
		r := rels[i]
		if r.Draft || r.TagName == "" {
			continue
		}
		slot := &stable
		if r.Prerelease {
			slot = &preview
		}
		if *slot == nil || Compare(NormalizeVersion(r.TagName), NormalizeVersion((*slot).TagName)) > 0 {
			rr := r
			*slot = &rr
		}
	}
	return stable, preview, nil
}

// TargetPath returns the absolute path of the running executable with symlinks
// resolved — the file that Apply will replace, wherever it sits on PATH.
func TargetPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// Apply downloads url and puts it in place of exePath.
//
//   - Unix: download to a temp file in the same directory, chmod +x, then
//     rename over the current binary (atomic on the same filesystem).
//   - Windows: a running .exe cannot be overwritten, so the current file is
//     renamed to mc-<currentVersion>.exe first and the download takes its place;
//     the renamed file can be deleted afterwards.
func Apply(ctx context.Context, url, exePath, currentVersion string) error {
	dir := filepath.Dir(exePath)

	if runtime.GOOS == "windows" {
		backup := filepath.Join(dir, fmt.Sprintf("mc-%s.exe", sanitize(currentVersion)))
		_ = os.Remove(backup)
		if err := os.Rename(exePath, backup); err != nil {
			return fmt.Errorf("setting aside the running executable: %w", err)
		}
		if err := download(ctx, url, exePath, 0o755); err != nil {
			_ = os.Rename(backup, exePath) // best-effort rollback
			return err
		}
		fmt.Fprintf(os.Stderr, "previous binary kept at %s\n", backup)
		return nil
	}

	tmp := filepath.Join(dir, ".mc-update-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if err := download(ctx, url, tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exePath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replacing %s (need write permission on its directory): %w", exePath, err)
	}
	return nil
}

func download(ctx context.Context, url, dest string, mode os.FileMode) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("writing %s: %w", dest, copyErr)
	}
	return closeErr
}

func sanitize(v string) string {
	v = strings.TrimSpace(v)
	repl := func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_', r == '+':
			return r
		default:
			return '-'
		}
	}
	if v == "" {
		return "previous"
	}
	return strings.Map(repl, v)
}
