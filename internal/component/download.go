package component

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// HTTPClient lets tests inject a fake HTTP client.
type HTTPClient interface {
	Get(url string) (*http.Response, error)
}

// Downloader performs verified binary downloads.
type Downloader struct {
	Client HTTPClient
	TmpDir string // defaults to os.TempDir() when empty
}

// Download fetches url to destPath, verifies sha256hex, sets mode 0755.
// Writes to a temp file first, verifies, then os.Rename (atomic on same fs).
// Removes the temp file on any error.
func (d Downloader) Download(url, sha256hex, destPath string) error {
	tmpDir := d.TmpDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}

	tmp, err := os.CreateTemp(tmpDir, "tgproxy-download-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}

	resp, err := d.Client.Get(url)
	if err != nil {
		cleanup()
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cleanup()
		return fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, url)
	}

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), resp.Body); err != nil {
		cleanup()
		return fmt.Errorf("download body: %w", err)
	}
	tmp.Close()

	computed := hex.EncodeToString(hash.Sum(nil))
	if computed != sha256hex {
		os.Remove(tmpPath)
		return fmt.Errorf("sha256 mismatch: got %s, want %s", computed, sha256hex)
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpPath, filepath.Clean(destPath)); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename to dest: %w", err)
	}

	return nil
}
