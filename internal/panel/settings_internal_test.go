package panel

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/stub"
)

func makeZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractZipRejectsOversizedFile(t *testing.T) {
	payload := makeZIP(t, map[string]string{
		"index.html": strings.Repeat("A", stub.MaxExtractedFileBytes+1),
	})

	err := extractZip(bytes.NewReader(payload), int64(len(payload)), t.TempDir())
	if err == nil {
		t.Fatal("expected oversized extracted file to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestExtractZipRejectsOversizedTotal(t *testing.T) {
	chunk := strings.Repeat("B", stub.MaxExtractedFileBytes-1024)
	payload := makeZIP(t, map[string]string{
		"index.html": chunk,
		"style.css":  chunk,
		"app.js":     chunk,
		"logo.svg":   chunk,
		"font.woff2": chunk,
	})

	err := extractZip(bytes.NewReader(payload), int64(len(payload)), t.TempDir())
	if err == nil {
		t.Fatal("expected oversized total extracted bytes to be rejected")
	}
	if !strings.Contains(err.Error(), "maximum extracted size") {
		t.Fatalf("expected total size limit error, got %v", err)
	}
}
