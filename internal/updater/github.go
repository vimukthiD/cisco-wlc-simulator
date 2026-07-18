package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// maxDownloadBytes caps a single asset download as a sanity guard against a
// runaway/hostile response. The real binaries are ~15 MB.
const maxDownloadBytes = 128 << 20 // 128 MiB

// apiBaseURL is the GitHub API root. Production builds always use the real
// GitHub host; it is only overridable in test builds (the `updatetest` build
// tag enables reading WLCSIM_UPDATE_API_BASE — see apibase_testhook.go) and by
// in-package tests. Release/default build targets never set that tag, so a
// released binary can never be pointed at a different host.
var apiBaseURL = "https://api.github.com"

// Asset is a single downloadable file attached to a GitHub release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Release is the subset of the GitHub releases API we care about.
type Release struct {
	TagName    string  `json:"tag_name"`
	HTMLURL    string  `json:"html_url"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
}

// asset returns the named asset, if present.
func (r *Release) asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// fetchLatestRelease queries the GitHub "latest release" endpoint. That
// endpoint already excludes drafts and pre-releases, so a successful result is
// always a stable release.
func fetchLatestRelease(ctx context.Context, repo string, hc *http.Client) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", apiBaseURL, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wlcsim-updater")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// ok
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, fmt.Errorf("GitHub rate limit reached; try again later")
	case http.StatusNotFound:
		return nil, fmt.Errorf("no published release found for %s", repo)
	default:
		return nil, fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}

	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("release has no tag name")
	}
	return &rel, nil
}

// fetchChecksums downloads and parses a `sha256sum`-formatted checksums asset
// into a map of filename -> lowercase hex digest.
func fetchChecksums(ctx context.Context, url string, hc *http.Client) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "wlcsim-updater")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checksums download returned HTTP %d", resp.StatusCode)
	}

	sums := make(map[string]string)
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		// "<hexdigest>  <filename>" — filename may be prefixed with '*' for
		// binary mode; strip it.
		name := strings.TrimPrefix(fields[1], "*")
		sums[name] = strings.ToLower(fields[0])
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	if len(sums) == 0 {
		return nil, fmt.Errorf("checksums file was empty")
	}
	return sums, nil
}

// downloadFile streams url to destPath (0755), returning the file's SHA-256 as
// a lowercase hex string so the caller can verify it against the checksums.
func downloadFile(ctx context.Context, url, destPath string, hc *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "wlcsim-updater")

	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}

	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxDownloadBytes))
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(destPath)
		return "", fmt.Errorf("write download: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(destPath)
		return "", closeErr
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
