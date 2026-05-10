package panel

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func TestBridgePageFormsUseValidateCSRFField(t *testing.T) {
	var buf bytes.Buffer

	bridgePage(&buf, bridgePageData{
		CSRFField: CSRFField(),
		CSRFToken: "test-token",
	})

	html := buf.String()
	if !strings.Contains(html, `name="`+CSRFField()+`"`) {
		t.Fatalf("bridge page should render CSRF field %q:\n%s", CSRFField(), html)
	}
	if strings.Contains(html, `name="csrf_token"`) {
		t.Fatalf("bridge page rendered cookie name as form field:\n%s", html)
	}
}

func TestBridgePageDashboardLinkUsesPanelPath(t *testing.T) {
	var buf bytes.Buffer

	bridgePage(&buf, bridgePageData{
		CSRFField: CSRFField(),
		CSRFToken: "test-token",
		PanelPath: "/p-example/",
	})

	html := buf.String()
	if !strings.Contains(html, `href="/p-example/dashboard"`) {
		t.Fatalf("bridge page must link back inside panel path:\n%s", html)
	}
	if strings.Contains(html, `href="../dashboard"`) {
		t.Fatalf("bridge page must not use parent-relative dashboard link:\n%s", html)
	}
}

func TestSingboxDownloadIsVerifiedReleaseAsset(t *testing.T) {
	if !strings.Contains(singboxDownloadURL(), "/releases/download/") {
		t.Fatalf("sing-box URL must use a pinned release asset, got %q", singboxDownloadURL())
	}
	if !strings.HasSuffix(singboxDownloadURL(), ".tar.gz") {
		t.Fatalf("sing-box URL must point to the upstream linux archive, got %q", singboxDownloadURL())
	}
	if _, err := hex.DecodeString(singboxDownloadSHA256()); err != nil {
		t.Fatalf("sing-box SHA256 must be hex: %v", err)
	}
	if len(singboxDownloadSHA256()) != 64 {
		t.Fatalf("sing-box SHA256 length = %d, want 64", len(singboxDownloadSHA256()))
	}
}
