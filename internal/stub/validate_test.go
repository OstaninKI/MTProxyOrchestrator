package stub

import (
	"archive/zip"
	"bytes"
	"testing"
)

func makeZip(t *testing.T, files map[string]string) ([]byte, int64) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(content))
	}
	w.Close()
	b := buf.Bytes()
	return b, int64(len(b))
}

func TestValidateValidZip(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"index.html": "<html><body>Hello</body></html>",
		"style.css":  "body { color: red; }",
		"logo.svg":   "<svg></svg>",
	})
	errs := Validate(bytes.NewReader(data), size)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidateOversized(t *testing.T) {
	data, _ := makeZip(t, map[string]string{
		"index.html": "<html></html>",
	})
	size := int64(MaxZipSize + 1)
	errs := Validate(bytes.NewReader(data), size)
	if len(errs) == 0 {
		t.Fatal("expected at least one error for oversized archive")
	}
	if errs[0].File != "" {
		t.Errorf("expected archive-level error (empty File field), got File=%q", errs[0].File)
	}
}

func TestValidatePathTraversal(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"../etc/passwd": "root:x:0:0",
	})
	errs := Validate(bytes.NewReader(data), size)
	if len(errs) == 0 {
		t.Fatal("expected at least one error for path traversal")
	}
	found := false
	for _, e := range errs {
		if e.File == "../etc/passwd" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error for file ../etc/passwd, got: %v", errs)
	}
}

func TestValidateAbsolutePath(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"/etc/passwd": "root:x:0:0",
	})
	errs := Validate(bytes.NewReader(data), size)
	if len(errs) == 0 {
		t.Fatal("expected at least one error for absolute path")
	}
}

func TestValidateSymlink(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fh := &zip.FileHeader{
		Name:           "link.html",
		Method:         zip.Store,
		CreatorVersion: 3 << 8, // upper byte 3 = Unix, required for ExternalAttrs interpretation
	}
	// Unix symlink type bits: 0xA000 (octal 0120000) in the upper 16 bits of ExternalAttrs
	fh.ExternalAttrs = 0xA000 << 16
	_, err := w.CreateHeader(fh)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	data := buf.Bytes()
	size := int64(len(data))

	errs := Validate(bytes.NewReader(data), size)
	if len(errs) == 0 {
		t.Fatal("expected at least one error for symlink")
	}
	found := false
	for _, e := range errs {
		if e.File == "link.html" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error for symlink file, got: %v", errs)
	}
}

func TestValidateDisallowedExtension(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"script.php": "<?php echo 'hello'; ?>",
	})
	errs := Validate(bytes.NewReader(data), size)
	if len(errs) == 0 {
		t.Fatal("expected at least one error for disallowed extension")
	}
	found := false
	for _, e := range errs {
		if e.File == "script.php" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error for file script.php, got: %v", errs)
	}
}

func TestValidateExternalURLInHTML(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"index.html": `<html><body><img src="https://example.com/logo.png"></body></html>`,
	})
	errs := Validate(bytes.NewReader(data), size)
	if len(errs) == 0 {
		t.Fatal("expected at least one error for external URL in HTML")
	}
	found := false
	for _, e := range errs {
		if e.File == "index.html" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error for file index.html, got: %v", errs)
	}
}

func TestValidateExternalURLInCSS(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"style.css": `body { background: url("https://fonts.googleapis.com/css2?family=Roboto"); }`,
	})
	errs := Validate(bytes.NewReader(data), size)
	if len(errs) == 0 {
		t.Fatal("expected at least one error for external URL in CSS")
	}
	found := false
	for _, e := range errs {
		if e.File == "style.css" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error for file style.css, got: %v", errs)
	}
}

func TestValidateMultipleErrors(t *testing.T) {
	// One path traversal entry + one external URL entry
	data, size := makeZip(t, map[string]string{
		"../evil.html": "<html></html>",
		"index.html":   `<html><img src="https://evil.com/img.png"></html>`,
	})
	errs := Validate(bytes.NewReader(data), size)
	if len(errs) < 2 {
		t.Errorf("expected at least 2 errors, got %d: %v", len(errs), errs)
	}
}
