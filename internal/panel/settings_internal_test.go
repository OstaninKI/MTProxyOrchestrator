package panel

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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

type countingReader struct {
	r    *bytes.Reader
	read int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.read += int64(n)
	return n, err
}

func (r *countingReader) Close() error { return nil }

func oversizedMultipartBody(t *testing.T) ([]byte, string) {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("stub_zip", "stub.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, strings.NewReader(strings.Repeat("A", maxUploadBytes+1024*1024))); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

func TestStubUploadSizeLimitAppliesBeforeCSRFFormParsing(t *testing.T) {
	body, contentType := oversizedMultipartBody(t)
	reader := &countingReader{r: bytes.NewReader(body)}
	req := httptest.NewRequest(http.MethodPost, "/settings/stubs/upload", reader)
	req.Header.Set("Content-Type", contentType)
	req.Body = reader
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok"})

	srv := &Server{Secure: false}
	w := httptest.NewRecorder()
	srv.handleSettingsStubUpload(w, req)

	if reader.read > maxUploadBytes+4096+1024 {
		t.Fatalf("handler read %d bytes before enforcing upload limit", reader.read)
	}
}
