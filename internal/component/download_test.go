package component_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/component"
)

func TestDownloadSuccess(t *testing.T) {
	data := []byte("fake binary content")
	h := sha256.Sum256(data)
	expectedSHA := hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "binary")

	dl := component.Downloader{
		Client: http.DefaultClient,
		TmpDir: t.TempDir(),
	}

	if err := dl.Download(srv.URL+"/binary", expectedSHA, destPath); err != nil {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "binary")

	dl := component.Downloader{
		Client: http.DefaultClient,
		TmpDir: t.TempDir(),
	}

	if err := dl.Download(srv.URL+"/binary", "deadbeef", destPath); err == nil {
		t.Fatal("expected error, got nil")
	}

	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatal("expected dest file to not exist")
	}
}

func TestDownloadSHA256Mismatch(t *testing.T) {
	data := []byte("fake binary content")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "binary")

	dl := component.Downloader{
		Client: http.DefaultClient,
		TmpDir: t.TempDir(),
	}

	if err := dl.Download(srv.URL+"/binary", "wrongsha256hex", destPath); err == nil {
		t.Fatal("expected error, got nil")
	}

	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatal("expected dest file to not exist")
	}
}

func TestDownloadBodyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("partial"))
		// Close the connection abruptly by hijacking
		if h, ok := w.(http.Hijacker); ok {
			conn, _, _ := h.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "binary")

	// Use a client with no retry so the broken connection surfaces as an error.
	// We need to close the connection mid-stream; use a server that writes
	// a Content-Length larger than what it actually sends.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("partial"))
		// Hijack and close to simulate abrupt disconnect
		if h, ok := w.(http.Hijacker); ok {
			conn, _, _ := h.Hijack()
			conn.Close()
		}
	}))
	defer srv2.Close()

	dl := component.Downloader{
		Client: http.DefaultClient,
		TmpDir: t.TempDir(),
	}

	// With Content-Length mismatch, the HTTP client will return an error on read.
	err := dl.Download(srv2.URL+"/binary", "anyhex", destPath)
	if err == nil {
		t.Fatal("expected error for broken body, got nil")
	}

	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Fatal("expected dest file to not exist after body error")
	}
}
