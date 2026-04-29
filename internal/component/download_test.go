package component_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/component"
)

type fakeHTTPClient struct {
	requested  bool
	statusCode int
	body       io.ReadCloser
	err        error
}

func (f *fakeHTTPClient) Get(url string) (*http.Response, error) {
	f.requested = true
	if f.err != nil {
		return nil, f.err
	}
	statusCode := f.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	body := f.body
	if body == nil {
		body = io.NopCloser(strings.NewReader(""))
	}
	return &http.Response{
		StatusCode: statusCode,
		Body:       body,
	}, nil
}

type failingReadCloser struct{}

func (failingReadCloser) Read(p []byte) (int, error) {
	return 0, errors.New("read failed")
}

func (failingReadCloser) Close() error {
	return nil
}

func TestDownloadSuccess(t *testing.T) {
	data := []byte("fake binary content")
	h := sha256.Sum256(data)
	expectedSHA := hex.EncodeToString(h[:])

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "binary")

	dl := component.Downloader{
		Client: &fakeHTTPClient{
			body: io.NopCloser(strings.NewReader(string(data))),
		},
		TmpDir: t.TempDir(),
	}

	if err := dl.Download("https://example.test/binary", expectedSHA, destPath); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("dest file not found: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("content mismatch: got %q, want %q", got, data)
	}

	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Mode()&0755 != 0755 {
		t.Fatalf("unexpected mode: %v", info.Mode())
	}
}

func TestDownloadHTTP404(t *testing.T) {
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "binary")

	dl := component.Downloader{
		Client: &fakeHTTPClient{statusCode: http.StatusNotFound},
		TmpDir: t.TempDir(),
	}

	if err := dl.Download("https://example.test/binary", strings.Repeat("0", 64), destPath); err == nil {
		t.Fatal("expected error, got nil")
	}

	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatal("expected dest file to not exist")
	}
}

func TestDownloadSHA256Mismatch(t *testing.T) {
	data := []byte("fake binary content")

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "binary")

	dl := component.Downloader{
		Client: &fakeHTTPClient{
			body: io.NopCloser(strings.NewReader(string(data))),
		},
		TmpDir: t.TempDir(),
	}

	if err := dl.Download("https://example.test/binary", strings.Repeat("0", 64), destPath); err == nil {
		t.Fatal("expected error, got nil")
	}

	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatal("expected dest file to not exist")
	}
}

func TestDownloadRequiresSHA256BeforeRequest(t *testing.T) {
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "binary")
	client := &fakeHTTPClient{}

	dl := component.Downloader{
		Client: client,
		TmpDir: t.TempDir(),
	}

	if err := dl.Download("https://example.test/binary", "", destPath); err == nil {
		t.Fatal("expected error for empty SHA256, got nil")
	}
	if client.requested {
		t.Fatal("download should reject an empty SHA256 before making a request")
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatal("expected dest file to not exist")
	}
}

func TestDownloadBodyError(t *testing.T) {
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "binary")

	dl := component.Downloader{
		Client: &fakeHTTPClient{body: failingReadCloser{}},
		TmpDir: t.TempDir(),
	}

	err := dl.Download("https://example.test/binary", strings.Repeat("0", 64), destPath)
	if err == nil {
		t.Fatal("expected error for broken body, got nil")
	}

	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatal("expected dest file to not exist after body error")
	}
}

func TestDownloadTarGzBinaryExtractsMember(t *testing.T) {
	archive := tarGz(t, map[string]string{
		"sing-box-1.13.11-linux-amd64/LICENSE":  "license",
		"sing-box-1.13.11-linux-amd64/sing-box": "binary content",
	})
	h := sha256.Sum256(archive)
	destPath := filepath.Join(t.TempDir(), "sing-box")

	dl := component.Downloader{
		Client: &fakeHTTPClient{
			body: io.NopCloser(bytes.NewReader(archive)),
		},
		TmpDir: t.TempDir(),
	}

	if err := dl.DownloadTarGzBinary("https://example.test/sing-box.tar.gz", hex.EncodeToString(h[:]), "sing-box", destPath); err != nil {
		t.Fatalf("DownloadTarGzBinary: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary content" {
		t.Fatalf("extracted content = %q", got)
	}
	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o, want 755", info.Mode().Perm())
	}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(body)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
