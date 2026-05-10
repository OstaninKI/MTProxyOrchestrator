package panel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	githubTreeURL  = "https://api.github.com/repos/learning-zone/website-templates/git/trees/main?recursive=1"
	githubRawBase  = "https://raw.githubusercontent.com/learning-zone/website-templates/main/"
	treeCacheTTL   = 30 * time.Minute
	maxRemoteFile  = 10 << 20 // 10 MB per file
	maxRemoteTotal = 50 << 20 // 50 MB total per template
)

// remoteHTTPClient is used for all GitHub API and raw file requests.
// Injectable in tests.
var remoteHTTPClient HTTPDoer = &http.Client{Timeout: 30 * time.Second}

// HTTPDoer is the minimal interface for making HTTP requests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
	Get(url string) (*http.Response, error)
}

// RemoteStubTemplate is a template directory available from the remote GitHub repo.
type RemoteStubTemplate struct {
	Name string
}

type githubTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
}

type githubTreeResponse struct {
	Tree      []githubTreeEntry `json:"tree"`
	Truncated bool              `json:"truncated"`
}

var (
	treeCacheMu      sync.Mutex
	treeCacheEntries []githubTreeEntry
	treeCachedAt     time.Time
)

// fetchOrGetTree returns cached tree entries or fetches fresh ones from GitHub.
func fetchOrGetTree(client HTTPDoer) ([]githubTreeEntry, error) {
	treeCacheMu.Lock()
	defer treeCacheMu.Unlock()

	if treeCacheEntries != nil && time.Since(treeCachedAt) < treeCacheTTL {
		return treeCacheEntries, nil
	}

	req, err := http.NewRequest("GET", githubTreeURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch GitHub tree: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var tree githubTreeResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&tree); err != nil {
		return nil, fmt.Errorf("decode GitHub tree: %w", err)
	}
	if tree.Truncated {
		return nil, fmt.Errorf("GitHub tree response was truncated")
	}

	treeCacheEntries = tree.Tree
	treeCachedAt = time.Now()
	return treeCacheEntries, nil
}

// fetchRemoteTemplateList returns the list of top-level template directories.
func fetchRemoteTemplateList(client HTTPDoer) ([]RemoteStubTemplate, error) {
	entries, err := fetchOrGetTree(client)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var out []RemoteStubTemplate
	for _, e := range entries {
		if e.Type == "tree" && !strings.Contains(e.Path, "/") && !seen[e.Path] {
			seen[e.Path] = true
			out = append(out, RemoteStubTemplate{Name: e.Path})
		}
	}
	return out, nil
}

// downloadRemoteTemplate downloads all blob files for the named template into destDir.
// Returns an error if no files were found (unknown template name).
func downloadRemoteTemplate(client HTTPDoer, name, destDir string) error {
	entries, err := fetchOrGetTree(client)
	if err != nil {
		return err
	}

	prefix := name + "/"
	var total int64
	var count int
	for _, e := range entries {
		if e.Type != "blob" || !strings.HasPrefix(e.Path, prefix) {
			continue
		}
		relPath := strings.TrimPrefix(e.Path, prefix)
		destPath := filepath.Join(destDir, relPath)

		if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
			return fmt.Errorf("create dirs for %s: %w", relPath, err)
		}

		rawURL := githubRawBase + e.Path
		n, err := downloadFileTo(client, rawURL, destPath)
		if err != nil {
			return fmt.Errorf("download %s: %w", relPath, err)
		}
		total += n
		count++
		if total > maxRemoteTotal {
			return fmt.Errorf("template exceeds maximum download size of %d MB", maxRemoteTotal>>20)
		}
	}
	if count == 0 {
		return fmt.Errorf("template %q not found in repository", name)
	}
	return nil
}

// downloadFileTo fetches url and writes it to destPath, returning bytes written.
func downloadFileTo(client HTTPDoer, url, destPath string) (int64, error) {
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxRemoteFile))
	closeErr := f.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	return n, closeErr
}
