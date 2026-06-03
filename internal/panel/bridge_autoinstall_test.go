package panel

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

// recordingBridgeExec captures Download calls so tests can assert that sing-box
// is fetched on demand. All other operations are no-ops.
type recordingBridgeExec struct {
	downloads []downloadCall
}

type downloadCall struct {
	url, sha256, dest string
}

func (e *recordingBridgeExec) WriteFile(string, []byte, os.FileMode) error { return nil }
func (e *recordingBridgeExec) Download(url, sha256, dest string) error {
	e.downloads = append(e.downloads, downloadCall{url, sha256, dest})
	return nil
}
func (e *recordingBridgeExec) EnableService(string) error         { return nil }
func (e *recordingBridgeExec) StartService(string) error          { return nil }
func (e *recordingBridgeExec) StopService(string) error           { return nil }
func (e *recordingBridgeExec) DisableService(string) error        { return nil }
func (e *recordingBridgeExec) ReloadService(string) error         { return nil }
func (e *recordingBridgeExec) ServiceActive(string) (bool, error) { return false, nil }

func newAutoInstallServer(t *testing.T, exec *recordingBridgeExec, installed bool) *Server {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	dir := t.TempDir()
	return &Server{
		DB:        d,
		PanelPath: "/p-example/",
		BridgeCfg: &BridgeConfig{
			Paths: config.InstallPaths{
				OutboundsJSON: filepath.Join(dir, "outbounds.json"),
				SingboxBin:    filepath.Join(dir, "sing-box"),
			},
		},
		SingboxActive:    func() bool { return false },
		SingboxInstalled: func() bool { return installed },
		BridgeExec:       exec,
	}
}

func postAddNode(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	const csrf = "tok"
	shareURL := "vless://0498a3ad-01c8-4cfe-8317-8793b5e9dfad@v2.pe4enk0.com:443?security=reality&pbk=TBuzyFsSS8dSzrL2O7lOyeDvBrcucEpSmipfYY5tMB0&sid=992a508f30&sni=www.nvidia.com&flow=xtls-rprx-vision&fp=chrome#node"
	form := url.Values{"_csrf": {csrf}, "share_url": {shareURL}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	w := httptest.NewRecorder()
	srv.handleBridgeAddNode(w, req)
	return w
}

func TestAddNodeDownloadsSingboxWhenMissing(t *testing.T) {
	exec := &recordingBridgeExec{}
	srv := newAutoInstallServer(t, exec, false /* not installed */)

	w := postAddNode(t, srv)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d body: %s", w.Code, w.Body.String())
	}
	if len(exec.downloads) != 1 {
		t.Fatalf("want exactly 1 sing-box download, got %d", len(exec.downloads))
	}
	dl := exec.downloads[0]
	if dl.url != singboxDownloadURL() {
		t.Errorf("download url = %q, want %q", dl.url, singboxDownloadURL())
	}
	if dl.sha256 != singboxDownloadSHA256() {
		t.Errorf("download sha256 = %q, want pinned sha", dl.sha256)
	}
	if dl.dest != srv.bridgePaths().SingboxBin {
		t.Errorf("download dest = %q, want %q", dl.dest, srv.bridgePaths().SingboxBin)
	}
}

func TestAddNodeSkipsDownloadWhenInstalled(t *testing.T) {
	exec := &recordingBridgeExec{}
	srv := newAutoInstallServer(t, exec, true /* already installed */)

	w := postAddNode(t, srv)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d body: %s", w.Code, w.Body.String())
	}
	if len(exec.downloads) != 0 {
		t.Fatalf("want no download when sing-box already installed, got %d", len(exec.downloads))
	}
}
