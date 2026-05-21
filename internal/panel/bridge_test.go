package panel

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
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

func TestBridgePageUsesPanelScopedRoutesWithoutInlineStyle(t *testing.T) {
	var buf bytes.Buffer

	bridgePage(&buf, bridgePageData{
		CSRFField: CSRFField(),
		CSRFToken: "test-token",
		PanelPath: "/p-example/",
		Nodes: []bridge.Node{
			{ID: 7, Tag: "de-1", Type: "vless-reality", Host: "de.example", Port: 443, Enabled: true, LastLatency: 84},
		},
	})

	html := buf.String()
	for _, want := range []string{
		`class="card bridge-banner"`,
		`class="seg bridge-seg"`,
		`href="#add-node"`,
		`class="btn" data-variant="primary" href="#add-node"`,
		`id="nodes"`,
		`id="routing-strategy"`,
		`id="mode-control"`,
		`class="badge ok">enabled</span>`,
		`class="badge ok">vless-reality</span>`,
		`class="bridge-latency"`,
		`class="ops-meter bridge-latency-meter"`,
		`action="/p-example/bridge/nodes/add"`,
		`action="/p-example/bridge/nodes/add-manual"`,
		`action="/p-example/bridge/nodes/7/toggle"`,
		`action="/p-example/bridge/nodes/7/ping"`,
		`href="/p-example/bridge/nodes/7/edit"`,
		`action="/p-example/bridge/nodes/7/delete"`,
		`action="/p-example/bridge/strategy"`,
		`action="/p-example/bridge/enable"`,
		`action="/p-example/bridge/disable"`,
		`class="page-head"`,
		`class="ring-lite mono"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("bridge page missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<style>") {
		t.Fatalf("bridge page must not render inline style:\n%s", html)
	}
}

func TestBridgePageMarksBridgeNavActive(t *testing.T) {
	var buf bytes.Buffer

	bridgePage(&buf, bridgePageData{
		CSRFField: CSRFField(),
		CSRFToken: "test-token",
		PanelPath: "/p-example/",
	})

	html := buf.String()
	if !strings.Contains(html, `class="nav-item" data-active="true" href="/p-example/bridge"`) {
		t.Fatalf("bridge page must mark bridge nav item active:\n%s", html)
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
