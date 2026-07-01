package panel

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
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

func multipartZipBody(t *testing.T, payload []byte) ([]byte, string) {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("stub_zip", "stub.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
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

func TestStubUploadRejectsActiveContent(t *testing.T) {
	payload := makeZIP(t, map[string]string{
		"index.html": `<html><body><script>fetch('/p-example/users')</script></body></html>`,
	})
	body, contentType := multipartZipBody(t, payload)
	req := httptest.NewRequest(http.MethodPost, "/settings/stubs/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok"})
	req.Form = map[string][]string{CSRFField(): {"tok"}}

	srv := &Server{Secure: false}
	w := httptest.NewRecorder()
	srv.handleSettingsStubUpload(w, req)

	if !strings.Contains(w.Body.String(), "active content is not allowed") {
		t.Fatalf("expected active content validation error, got status %d body: %s", w.Code, w.Body.String())
	}
}

type fakeRemoteClient struct {
	tree []githubTreeEntry
	raw  map[string]string
}

func (c fakeRemoteClient) Do(req *http.Request) (*http.Response, error) {
	// Route by URL: the tree API call returns the tree JSON, raw file fetches
	// return their canned body — mirroring the real client and Get() below.
	if req.URL.String() == githubTreeURL {
		body, err := json.Marshal(githubTreeResponse{Tree: c.tree})
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	}
	body, ok := c.raw[req.URL.String()]
	if !ok {
		return nil, fmt.Errorf("unexpected raw URL %s", req.URL.String())
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func (c fakeRemoteClient) Get(url string) (*http.Response, error) {
	body, ok := c.raw[url]
	if !ok {
		return nil, fmt.Errorf("unexpected raw URL %s", url)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

func resetRemoteTreeCacheForTest(t *testing.T) {
	t.Helper()
	treeCacheMu.Lock()
	treeCacheEntries = nil
	treeCachedAt = time.Time{}
	treeCacheMu.Unlock()
}

func TestDownloadRemoteTemplateRejectsTraversalPaths(t *testing.T) {
	resetRemoteTreeCacheForTest(t)
	client := fakeRemoteClient{
		tree: []githubTreeEntry{{Path: "sample/../escape.html", Type: "blob"}},
		raw: map[string]string{
			githubRawBase + "sample/../escape.html": "<html></html>",
		},
	}
	tmp := t.TempDir()

	err := downloadRemoteTemplate(context.Background(), client, "sample", tmp)
	if err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "..", "escape.html")); !os.IsNotExist(statErr) {
		t.Fatalf("remote traversal wrote outside destination: %v", statErr)
	}
}

func TestRemoteStubApplyRejectsActiveContentBeforeApply(t *testing.T) {
	resetRemoteTreeCacheForTest(t)
	oldClient := remoteHTTPClient
	oldApply := applyStubTemplate
	t.Cleanup(func() {
		remoteHTTPClient = oldClient
		applyStubTemplate = oldApply
		resetRemoteTreeCacheForTest(t)
	})
	remoteHTTPClient = fakeRemoteClient{
		tree: []githubTreeEntry{
			{Path: "sample", Type: "tree"},
			{Path: "sample/index.html", Type: "blob"},
		},
		raw: map[string]string{
			githubRawBase + "sample/index.html": `<html><body><script>fetch('/p-example/users')</script></body></html>`,
		},
	}
	applied := false
	applyStubTemplate = func(_, _ string) error {
		applied = true
		return nil
	}

	testDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testDB.Close() })
	srv := &Server{DB: testDB, Secure: false, PanelPath: "/p-example/", SettingsCfg: &SettingsConfig{WebRoot: t.TempDir()}}
	form := url.Values{CSRFField(): {"tok"}, "template": {"sample"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/stubs/remote-apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok"})
	rec := httptest.NewRecorder()

	srv.handleSettingsStubRemoteApply(rec, req)

	if applied {
		t.Fatal("remote stub with active content reached applyStubTemplate")
	}
	if !strings.Contains(rec.Body.String(), "active content is not allowed") {
		t.Fatalf("expected validation error, got status %d body: %s", rec.Code, rec.Body.String())
	}
}

// TestDownloadRemoteTemplateRespectsCancelledContext verifies the download path
// honors the context deadline instead of compounding per-file timeouts across
// all template files. A cancelled context must abort before writing files.
func TestDownloadRemoteTemplateRespectsCancelledContext(t *testing.T) {
	resetRemoteTreeCacheForTest(t)
	t.Cleanup(func() { resetRemoteTreeCacheForTest(t) })
	client := fakeRemoteClient{
		tree: []githubTreeEntry{
			{Path: "sample/index.html", Type: "blob"},
		},
		raw: map[string]string{
			githubRawBase + "sample/index.html": "<html></html>",
		},
	}
	tmp := t.TempDir()
	// Pre-cancel the context: the download must fail with ctx.Err().
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := downloadRemoteTemplate(ctx, client, "sample", tmp)
	if err == nil {
		t.Fatal("expected cancelled context to abort download, got nil")
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "index.html")); statErr == nil {
		t.Fatal("file was written despite cancelled context")
	}
}
