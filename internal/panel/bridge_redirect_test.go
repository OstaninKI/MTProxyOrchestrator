package panel

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

// Regression: bridge POST handlers must redirect to the panel-scoped /bridge
// page, not a doubled "bridge/bridge" path (which 404s).
func TestBridgeAddNodeRedirectsToPanelBridge(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	srv := &Server{
		DB:        d,
		PanelPath: "/p-example/",
		BridgeCfg: &BridgeConfig{
			Paths: config.InstallPaths{
				OutboundsJSON:  filepath.Join(dir, "outbounds.json"),
				TeleproxyTOML:  filepath.Join(dir, "teleproxy.toml"),
				SingboxJSON:    filepath.Join(dir, "sing-box.json"),
				SingboxService: filepath.Join(dir, "sing-box.service"),
				SingboxBin:     filepath.Join(dir, "sing-box"),
			},
		},
		SingboxActive:    func() bool { return false },
		SingboxInstalled: func() bool { return true },
		BridgeExec:       newBridgeEnableExec(),
	}

	const csrf = "tok"
	shareURL := "vless://0498a3ad-01c8-4cfe-8317-8793b5e9dfad@v2.pe4enk0.com:443?encryption=none&flow=xtls-rprx-vision&fp=chrome&pbk=TBuzyFsSS8dSzrL2O7lOyeDvBrcucEpSmipfYY5tMB0&security=reality&sid=992a508f30&sni=www.nvidia.com&spx=%2F8b34eZuZnsTqweM&type=tcp#v2.pe4enk0.com-mtpr"

	form := url.Values{"_csrf": {csrf}, "share_url": {shareURL}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w := httptest.NewRecorder()

	srv.handleBridgeAddNode(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("add node: want 303, got %d body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/p-example/bridge" {
		t.Fatalf("add node redirect Location = %q, want %q (doubled path 404s)", got, "/p-example/bridge")
	}
}
