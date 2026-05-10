package component_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/component"
)

type fakeHTTPClient struct {
	requested  bool
	statusCode int
	body       io.ReadCloser
	err        error
}

type blockingRoundTripper struct{}

func (blockingRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	<-r.Context().Done()
	return nil, r.Context().Err()
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

// TestDownloadOversizedRejected verifies that downloads exceeding MaxSize are rejected.
func TestDownloadOversizedRejected(t *testing.T) {
	// 5 bytes of data with a MaxSize of 3 bytes — should be rejected.
	data := []byte("hello")
	h := sha256.Sum256(data)
	expectedSHA := hex.EncodeToString(h[:])

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "binary")

	dl := component.Downloader{
		Client: &fakeHTTPClient{
			body: io.NopCloser(bytes.NewReader(data)),
		},
		TmpDir:  t.TempDir(),
		MaxSize: 3,
	}

	err := dl.Download("https://example.test/binary", expectedSHA, destPath)
	if err == nil {
		t.Fatal("expected error for oversized download, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected 'exceeds maximum size' error, got: %v", err)
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatal("expected dest file to not exist after oversized download rejection")
	}
}

// TestDownloadTimeout verifies that downloads from a slow server are cancelled after the timeout.
func TestDownloadTimeout(t *testing.T) {
	dl := component.Downloader{
		Client: &http.Client{
			Timeout:   50 * time.Millisecond,
			Transport: blockingRoundTripper{},
		},
		TmpDir: t.TempDir(),
	}

	destPath := filepath.Join(t.TempDir(), "binary")
	err := dl.Download("https://example.test/binary", strings.Repeat("0", 64), destPath)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "Client.Timeout") {
		t.Fatalf("expected timeout error, got %v", err)
	}

	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatal("expected dest file to not exist after timeout")
	}
}

// TestDownloadTarGzMemberExtractedSizeLimit verifies the extractor refuses to write
// past MaxExtractedMemberBytes when a malicious archive's member produces excessive output.
func TestDownloadTarGzMemberExtractedSizeLimit(t *testing.T) {
	const memberName = "sing-box"
	const declaredSize = int64(component.MaxExtractedMemberBytes) + 100

	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "sing-box-evil/" + memberName,
		Mode:     0o755,
		Size:     declaredSize,
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 1<<16)
	var written int64
	for written < declaredSize {
		n := int64(len(chunk))
		if remaining := declaredSize - written; remaining < n {
			n = remaining
		}
		if _, err := tw.Write(chunk[:n]); err != nil {
			t.Fatal(err)
		}
		written += n
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	archive := raw.Bytes()
	h := sha256.Sum256(archive)
	destPath := filepath.Join(t.TempDir(), "sing-box")

	dl := component.Downloader{
		Client: &fakeHTTPClient{
			body: io.NopCloser(bytes.NewReader(archive)),
		},
		TmpDir:  t.TempDir(),
		MaxSize: int64(len(archive)) + 1,
	}

	err := dl.DownloadTarGzBinary("https://example.test/sing-box.tar.gz", hex.EncodeToString(h[:]), memberName, destPath)
	if err == nil {
		t.Fatal("expected error for oversized extracted member, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected 'exceeds limit' error, got: %v", err)
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatal("expected dest file to not exist after oversized extraction rejection")
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
