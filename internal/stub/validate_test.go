package stub

import (
	"archive/zip"
	"bytes"
	"strings"
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

func TestValidateRejectsJavaScriptFile(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"index.html": "<html><body>Hello</body></html>",
		"app.js":     "fetch('/p-example/users')",
	})
	errs := Validate(bytes.NewReader(data), size)
	if !hasValidationErrorForFile(errs, "app.js") {
		t.Fatalf("expected JavaScript file to be rejected, got: %v", errs)
	}
}

func TestValidateRejectsInlineScriptInHTML(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"index.html": `<html><body><script>fetch('/p-example/users')</script></body></html>`,
	})
	errs := Validate(bytes.NewReader(data), size)
	if !hasValidationErrorForFile(errs, "index.html") {
		t.Fatalf("expected inline script to be rejected, got: %v", errs)
	}
}

func TestValidateRejectsEventHandlerAttribute(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"index.html": `<html><body><img src="logo.png" onerror="fetch('/p-example/users')"></body></html>`,
	})
	errs := Validate(bytes.NewReader(data), size)
	if !hasValidationErrorForFile(errs, "index.html") {
		t.Fatalf("expected event handler attribute to be rejected, got: %v", errs)
	}
}

func TestValidateRejectsJavaScriptURL(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"index.html": `<html><body><a href="javascript:fetch('/p-example/users')">open</a></body></html>`,
	})
	errs := Validate(bytes.NewReader(data), size)
	if !hasValidationErrorForFile(errs, "index.html") {
		t.Fatalf("expected javascript URL to be rejected, got: %v", errs)
	}
}

func TestValidateRejectsActiveSVG(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"logo.svg": `<svg xmlns="http://www.w3.org/2000/svg"><script>fetch('/p-example/users')</script></svg>`,
	})
	errs := Validate(bytes.NewReader(data), size)
	if !hasValidationErrorForFile(errs, "logo.svg") {
		t.Fatalf("expected active SVG to be rejected, got: %v", errs)
	}
}

func TestValidateAllowsPassiveSVGNamespace(t *testing.T) {
	data, size := makeZip(t, map[string]string{
		"index.html": "<html><body></body></html>",
		"logo.svg":   `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/></svg>`,
	})
	errs := Validate(bytes.NewReader(data), size)
	if len(errs) != 0 {
		t.Fatalf("expected passive SVG namespace to be allowed, got: %v", errs)
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

func TestValidateLargeHTMLWithScriptRejected(t *testing.T) {
	// Files > 1 MB must still be scanned for active content.
	large := strings.Repeat("x", 1024*1024+1) + "<script>alert(1)</script>"
	data, size := makeZip(t, map[string]string{
		"index.html": large,
	})
	errs := Validate(bytes.NewReader(data), size)
	if !hasValidationErrorForFile(errs, "index.html") {
		t.Fatalf("expected large HTML with script to be rejected, got: %v", errs)
	}
}

func TestValidateCSSSlashSlashCommentAllowed(t *testing.T) {
	// CSS with // comments must not be falsely rejected as an external URL.
	data, size := makeZip(t, map[string]string{
		"index.html": "<html><body></body></html>",
		"style.css":  "body { color: red; } // fallback comment\n.foo { margin: 0; }",
	})
	errs := Validate(bytes.NewReader(data), size)
	if len(errs) != 0 {
		t.Fatalf("expected CSS with // comment to be allowed, got: %v", errs)
	}
}

func TestValidateProtocolRelativeURLRejected(t *testing.T) {
	// Protocol-relative URLs like //example.com/img.png must be rejected.
	data, size := makeZip(t, map[string]string{
		"index.html": `<html><body><img src="//example.com/img.png"></body></html>`,
	})
	errs := Validate(bytes.NewReader(data), size)
	if !hasValidationErrorForFile(errs, "index.html") {
		t.Fatalf("expected protocol-relative URL to be rejected, got: %v", errs)
	}
}

func TestValidateSVGSettingsTagAllowed(t *testing.T) {
	// <settings> must not be mistaken for SVG <set> animation element.
	data, size := makeZip(t, map[string]string{
		"index.html": "<html><body></body></html>",
		"logo.svg":   `<svg xmlns="http://www.w3.org/2000/svg"><settings><option/></settings></svg>`,
	})
	errs := Validate(bytes.NewReader(data), size)
	if len(errs) != 0 {
		t.Fatalf("expected SVG with <settings> tag to be allowed, got: %v", errs)
	}
}

func TestValidateSVGSetAnimationRejected(t *testing.T) {
	// SVG <set> animation element must be rejected.
	data, size := makeZip(t, map[string]string{
		"index.html": "<html><body></body></html>",
		"logo.svg":   `<svg xmlns="http://www.w3.org/2000/svg"><circle><set attributeName="r" to="10"/></circle></svg>`,
	})
	errs := Validate(bytes.NewReader(data), size)
	if !hasValidationErrorForFile(errs, "logo.svg") {
		t.Fatalf("expected SVG with <set> animation to be rejected, got: %v", errs)
	}
}

func TestValidateMissingRootHTMLRejected(t *testing.T) {
	// A ZIP with no .html/.htm file at the root must be rejected.
	data, size := makeZip(t, map[string]string{
		"style.css": "body { color: red; }",
		"logo.png":  "fakepng",
	})
	errs := Validate(bytes.NewReader(data), size)
	hasArchiveErr := false
	for _, e := range errs {
		if e.File == "" {
			hasArchiveErr = true
			break
		}
	}
	if !hasArchiveErr {
		t.Fatalf("expected archive-level error for missing root HTML, got: %v", errs)
	}
}

func hasValidationErrorForFile(errs []ValidationError, file string) bool {
	for _, e := range errs {
		if e.File == file {
			return true
		}
	}
	return false
}
