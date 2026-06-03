package component

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type internalFakeHTTPClient struct {
	body io.ReadCloser
}

func (f internalFakeHTTPClient) Get(string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: f.body}, nil
}

func TestDownloadTarGzBinaryFallsBackWhenRenameCrossesDevices(t *testing.T) {
	archive := internalTarGz(t, "sing-box-1.13.12-linux-amd64/sing-box", "binary content")
	h := sha256.Sum256(archive)
	destPath := filepath.Join(t.TempDir(), "sing-box")

	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		if filepath.Base(oldpath) != ".tgproxy-install-tmp" {
			return syscall.EXDEV
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = oldRename })

	dl := Downloader{
		Client: internalFakeHTTPClient{body: io.NopCloser(bytes.NewReader(archive))},
		TmpDir: t.TempDir(),
	}
	if err := dl.DownloadTarGzBinary("https://example.test/sing-box.tar.gz", hex.EncodeToString(h[:]), "sing-box", destPath); err != nil {
		t.Fatalf("DownloadTarGzBinary should copy across devices after EXDEV: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary content" {
		t.Fatalf("content = %q, want binary content", got)
	}
	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o, want 755", info.Mode().Perm())
	}
}

func TestDownloadFallsBackWhenRenameCrossesDevices(t *testing.T) {
	data := []byte("binary content")
	h := sha256.Sum256(data)
	destPath := filepath.Join(t.TempDir(), "binary")

	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		if filepath.Base(oldpath) != ".tgproxy-install-tmp" {
			return syscall.EXDEV
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = oldRename })

	dl := Downloader{
		Client: internalFakeHTTPClient{body: io.NopCloser(bytes.NewReader(data))},
		TmpDir: t.TempDir(),
	}
	if err := dl.Download("https://example.test/binary", hex.EncodeToString(h[:]), destPath); err != nil {
		t.Fatalf("Download should copy across devices after EXDEV: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("content = %q, want %q", got, data)
	}
}

func internalTarGz(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestHTTPClientAppliesDefaultTimeoutToZeroTimeoutHTTPClient(t *testing.T) {
	dl := Downloader{Client: &http.Client{}}

	client, ok := dl.httpClient().(*http.Client)
	if !ok {
		t.Fatal("expected httpClient to return *http.Client")
	}
	if client.Timeout != DefaultHTTPTimeout {
		t.Fatalf("timeout = %v, want %v", client.Timeout, DefaultHTTPTimeout)
	}
}
