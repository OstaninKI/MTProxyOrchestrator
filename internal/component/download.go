package component

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Download size limits.
const (
	// MaxBinaryBytes is the maximum size allowed for a direct binary download (200 MB).
	MaxBinaryBytes = 200 * 1024 * 1024
	// MaxArchiveBytes is the maximum size allowed for a tar.gz archive download (50 MB).
	MaxArchiveBytes = 50 * 1024 * 1024

	// DefaultHTTPTimeout is the timeout applied to each HTTP download request.
	DefaultHTTPTimeout = 10 * time.Minute
)

// HTTPClient lets tests inject a fake HTTP client.
type HTTPClient interface {
	Get(url string) (*http.Response, error)
}

// Downloader performs verified binary downloads.
type Downloader struct {
	Client  HTTPClient
	TmpDir  string // defaults to os.TempDir() when empty
	MaxSize int64  // maximum download size in bytes; 0 means use the default for the download type
}

// Download fetches url to destPath, verifies sha256hex, sets mode 0755.
// Writes to a temp file first, verifies, then os.Rename (atomic on same fs).
// Removes the temp file on any error.
// The download body is limited to MaxBinaryBytes (or d.MaxSize if set).
func (d Downloader) Download(url, sha256hex, destPath string) error {
	if err := validateSHA256Hex(sha256hex); err != nil {
		return err
	}

	tmpDir := d.TmpDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	maxSize := d.MaxSize
	if maxSize <= 0 {
		maxSize = MaxBinaryBytes
	}
	tmpPath, err := d.downloadVerified(url, sha256hex, tmpDir, maxSize)
	if err != nil {
		return err
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

// DownloadTarGzBinary fetches url, verifies sha256hex, extracts memberName from
// the tar.gz archive, and installs it at destPath with mode 0755.
func (d Downloader) DownloadTarGzBinary(url, sha256hex, memberName, destPath string) error {
	if err := validateSHA256Hex(sha256hex); err != nil {
		return err
	}
	if memberName == "" {
		return fmt.Errorf("archive member name is required")
	}

	tmpDir := d.TmpDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	maxSize := d.MaxSize
	if maxSize <= 0 {
		maxSize = MaxArchiveBytes
	}
	archive, err := d.downloadVerified(url, sha256hex, tmpDir, maxSize)
	if err != nil {
		return err
	}
	defer os.Remove(archive)

	tmp, err := os.CreateTemp(tmpDir, "tgproxy-extract-*")
	if err != nil {
		return fmt.Errorf("create extract temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}

	if err := extractTarGzMember(archive, memberName, tmp); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close extract temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(tmpPath, filepath.Clean(destPath)); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename to dest: %w", err)
	}
	return nil
}

func validateSHA256Hex(sha256hex string) error {
	decoded, err := hex.DecodeString(sha256hex)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("sha256 must be a 64-character hex string")
	}
	return nil
}

func (d Downloader) downloadVerified(url, sha256hex, tmpDir string, maxSize int64) (string, error) {
	client := d.httpClient()

	tmp, err := os.CreateTemp(tmpDir, "tgproxy-download-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	cleanup := func() {
		tmp.Close()
		os.Remove(tmpPath)
	}

	resp, err := client.Get(url)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cleanup()
		return "", fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, url)
	}

	// Limit the response body to maxSize+1 so that we can detect oversized downloads.
	limited := io.LimitReader(resp.Body, maxSize+1)
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hash), limited)
	if err != nil {
		cleanup()
		return "", fmt.Errorf("download body: %w", err)
	}
	if written > maxSize {
		cleanup()
		return "", fmt.Errorf("download exceeds maximum size of %d bytes", maxSize)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", err)
	}

	computed := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(computed, sha256hex) {
		os.Remove(tmpPath)
		return "", fmt.Errorf("sha256 mismatch: got %s, want %s", computed, sha256hex)
	}
	return tmpPath, nil
}

func (d Downloader) httpClient() HTTPClient {
	if d.Client == nil {
		return &http.Client{Timeout: DefaultHTTPTimeout}
	}
	httpClient, ok := d.Client.(*http.Client)
	if !ok || httpClient.Timeout > 0 {
		return d.Client
	}
	clone := *httpClient
	clone.Timeout = DefaultHTTPTimeout
	return &clone
}

func extractTarGzMember(archivePath, memberName string, dst io.Writer) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("archive member %q not found", memberName)
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != memberName {
			continue
		}
		if _, err := io.Copy(dst, tr); err != nil {
			return fmt.Errorf("extract %s: %w", memberName, err)
		}
		return nil
	}
}
