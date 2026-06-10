package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

const pe4enk0VLESSURL = "vless://0498a3ad-01c8-4cfe-8317-8793b5e9dfad@v2.pe4enk0.com:443?encryption=none&flow=xtls-rprx-vision&fp=chrome&pbk=TBuzyFsSS8dSzrL2O7lOyeDvBrcucEpSmipfYY5tMB0&security=reality&sid=992a508f30&sni=www.nvidia.com&spx=%2F8b34eZuZnsTqweM&type=tcp#v2.pe4enk0.com-mtpr"

func newBridgeTestServer(t *testing.T, nodePath string) *Server {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return &Server{
		DB:          database,
		PanelPath:   "/p-example/",
		RateLimiter: NewRateLimiter(),
		Secure:      false,
		BridgeCfg: &BridgeConfig{
			Paths: config.InstallPaths{
				OutboundsJSON: nodePath,
				SingboxJSON:   filepath.Join(filepath.Dir(nodePath), "sing-box.json"),
			},
		},
	}
}

func addAuthSession(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	if _, err := s.DB.Exec(`INSERT INTO admin(id, login, password_hash) VALUES(1,'admin','$2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuu')`); err != nil {
		// ignore duplicate insert in repeated helper calls
	}
	if _, err := s.DB.Exec(`INSERT INTO sessions(id, admin_id, expires_at, ip) VALUES('session',1,datetime('now','+1 day'),'127.0.0.1')`); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: "session", Path: s.PanelPath}
}

func writeNodeList(t *testing.T, path string, nl bridge.NodeList) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := nl.Save(path); err != nil {
		t.Fatal(err)
	}
}

func readNodeList(t *testing.T, path string) bridge.NodeList {
	t.Helper()
	nl, err := bridge.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return nl
}

func withRouteID(req *http.Request, id string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func TestHandleBridgeEnableExactVLESSURLSwitchesSingleToBridge(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	teleproxyPath := filepath.Join(dir, "teleproxy.toml")
	if err := os.WriteFile(teleproxyPath, []byte("port = 443\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newBridgeTestServer(t, nodePath)
	srv.BridgeCfg.Paths.TeleproxyTOML = teleproxyPath
	srv.BridgeCfg.Paths.SingboxService = filepath.Join(dir, "sing-box.service")
	srv.BridgeCfg.Paths.SingboxBin = filepath.Join(dir, "sing-box")
	exec := newBridgeEnableExec()
	srv.BridgeExec = exec
	sessionCookie := addAuthSession(t, srv)

	form := url.Values{CSRFField(): {"token"}, "vless_url": {pe4enk0VLESSURL}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/enable", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()

	srv.handleBridgeEnable(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303, body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/p-example/bridge" {
		t.Fatalf("Location = %q, want /p-example/bridge", got)
	}

	nl := readNodeList(t, nodePath)
	if len(nl.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1: %+v", len(nl.Nodes), nl.Nodes)
	}
	n := nl.Nodes[0]
	if n.Tag != "v2.pe4enk0.com-mtpr" || n.Host != "v2.pe4enk0.com" || n.Port != 443 {
		t.Fatalf("parsed node metadata wrong: %+v", n)
	}
	if n.UUID != "0498a3ad-01c8-4cfe-8317-8793b5e9dfad" || n.Flow != "xtls-rprx-vision" || n.Fingerprint != "chrome" {
		t.Fatalf("parsed VLESS fields wrong: %+v", n)
	}
	if n.SNI != "www.nvidia.com" || n.PublicKey != "TBuzyFsSS8dSzrL2O7lOyeDvBrcucEpSmipfYY5tMB0" || n.ShortID != "992a508f30" {
		t.Fatalf("parsed Reality fields wrong: %+v", n)
	}

	tp := string(exec.writes[teleproxyPath])
	if !strings.Contains(tp, `socks5 = "socks5://127.0.0.1:1080"`) {
		t.Fatalf("Teleproxy was not switched to Bridge SOCKS5 mode:\n%s", tp)
	}

	var rendered struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(exec.writes[srv.BridgeCfg.Paths.SingboxJSON], &rendered); err != nil {
		t.Fatalf("decode sing-box config: %v", err)
	}
	if len(rendered.Outbounds) == 0 {
		t.Fatal("sing-box config has no outbounds")
	}
	out := rendered.Outbounds[0]
	if out["type"] != "vless" || out["server"] != "v2.pe4enk0.com" || out["uuid"] != "0498a3ad-01c8-4cfe-8317-8793b5e9dfad" {
		t.Fatalf("sing-box VLESS outbound wrong: %+v", out)
	}
	tlsBlock, ok := out["tls"].(map[string]any)
	if !ok {
		t.Fatalf("sing-box VLESS outbound missing tls: %+v", out)
	}
	utlsBlock, ok := tlsBlock["utls"].(map[string]any)
	if !ok || utlsBlock["fingerprint"] != "chrome" {
		t.Fatalf("sing-box VLESS outbound missing utls chrome fingerprint: %+v", tlsBlock)
	}
	if !exec.services["sing-box.service"] || !exec.services["teleproxy.service"] {
		t.Fatalf("Bridge services were not active after enable: %+v", exec.services)
	}
}

type bridgeEnableExec struct {
	writes   map[string][]byte
	services map[string]bool
}

func newBridgeEnableExec() *bridgeEnableExec {
	return &bridgeEnableExec{
		writes:   make(map[string][]byte),
		services: make(map[string]bool),
	}
}

func (e *bridgeEnableExec) WriteFile(path string, data []byte, _ os.FileMode) error {
	e.writes[path] = append([]byte(nil), data...)
	return nil
}
func (e *bridgeEnableExec) Download(_, _, _ string) error { return nil }
func (e *bridgeEnableExec) EnableService(_ string) error  { return nil }
func (e *bridgeEnableExec) StartService(name string) error {
	e.services[name] = true
	return nil
}
func (e *bridgeEnableExec) StopService(name string) error {
	e.services[name] = false
	return nil
}
func (e *bridgeEnableExec) DisableService(_ string) error { return nil }
func (e *bridgeEnableExec) ReloadService(name string) error {
	e.services[name] = true
	return nil
}
func (e *bridgeEnableExec) ServiceActive(name string) (bool, error) {
	return e.services[name], nil
}

func TestHandleBridgeAddNodeExactVLESSURLSwitchesSingleToBridge(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	teleproxyPath := filepath.Join(dir, "teleproxy.toml")
	if err := os.WriteFile(teleproxyPath, []byte("port = 443\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newBridgeTestServer(t, nodePath)
	srv.BridgeCfg.Paths.TeleproxyTOML = teleproxyPath
	srv.BridgeCfg.Paths.SingboxService = filepath.Join(dir, "sing-box.service")
	srv.BridgeCfg.Paths.SingboxBin = filepath.Join(dir, "sing-box")
	srv.SingboxActive = func() bool { return false }
	srv.SingboxInstalled = func() bool { return true }
	exec := newBridgeEnableExec()
	srv.BridgeExec = exec
	sessionCookie := addAuthSession(t, srv)

	form := url.Values{CSRFField(): {"token"}, "share_url": {pe4enk0VLESSURL}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()

	srv.handleBridgeAddNode(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303, body: %s", rec.Code, rec.Body.String())
	}

	tp := string(exec.writes[teleproxyPath])
	if !strings.Contains(tp, `socks5 = "socks5://127.0.0.1:1080"`) {
		t.Fatalf("add-node did not switch Teleproxy to Bridge SOCKS5 mode:\n%s", tp)
	}
	if !exec.services["sing-box.service"] || !exec.services["teleproxy.service"] {
		t.Fatalf("add-node did not activate Bridge services: %+v", exec.services)
	}
	nl := readNodeList(t, nodePath)
	if len(nl.Nodes) != 1 || nl.Nodes[0].Tag != "v2.pe4enk0.com-mtpr" {
		t.Fatalf("node list after add = %+v, want exact VLESS node", nl.Nodes)
	}
}

func TestHandleBridgeEditNodeRollsBackNodeFileOnRerenderFailure(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	writeNodeList(t, nodePath, bridge.NodeList{
		Nodes: []bridge.Node{{ID: 1, Tag: "old", Host: "old.example", Port: 443, Enabled: true}},
	})
	srv := newBridgeTestServer(t, nodePath)
	sessionCookie := addAuthSession(t, srv)

	oldRerender := rerenderSingboxIfActiveFn
	rerenderSingboxIfActiveFn = func(*Server, bridge.NodeList) error { return assertErr("rerender failed") }
	t.Cleanup(func() { rerenderSingboxIfActiveFn = oldRerender })

	form := url.Values{
		CSRFField():          {"token"},
		"tag":                {"new"},
		"host":               {"new.example"},
		"port":               {"8443"},
		"sni":                {"sni.example"},
		"flow":               {"flow"},
		"method":             {"method"},
		"congestion_control": {"bbr"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/1/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	req = withRouteID(req, "1")
	rec := httptest.NewRecorder()

	srv.handleBridgeEditNode(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	got := readNodeList(t, nodePath)
	if got.Nodes[0].Tag != "old" || got.Nodes[0].Host != "old.example" || got.Nodes[0].Port != 443 {
		t.Fatalf("node file was not rolled back: %+v", got.Nodes[0])
	}
}

func TestHandleBridgeToggleNodeRollsBackNodeFileOnRerenderFailure(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	writeNodeList(t, nodePath, bridge.NodeList{
		Nodes: []bridge.Node{{ID: 1, Tag: "node", Host: "host", Port: 443, Enabled: true}},
	})
	srv := newBridgeTestServer(t, nodePath)
	sessionCookie := addAuthSession(t, srv)

	oldRerender := rerenderSingboxIfActiveFn
	rerenderSingboxIfActiveFn = func(*Server, bridge.NodeList) error { return assertErr("rerender failed") }
	t.Cleanup(func() { rerenderSingboxIfActiveFn = oldRerender })

	form := url.Values{CSRFField(): {"token"}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/1/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	req = withRouteID(req, "1")
	rec := httptest.NewRecorder()

	srv.handleBridgeToggleNode(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	got := readNodeList(t, nodePath)
	if !got.Nodes[0].Enabled {
		t.Fatalf("toggle was not rolled back: %+v", got.Nodes[0])
	}
}

func TestHandleBridgeDeleteNodeRollsBackNodeFileOnRerenderFailure(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	writeNodeList(t, nodePath, bridge.NodeList{
		Nodes: []bridge.Node{{ID: 1, Tag: "node", Host: "host", Port: 443, Enabled: true}},
	})
	srv := newBridgeTestServer(t, nodePath)
	sessionCookie := addAuthSession(t, srv)

	oldRerender := rerenderSingboxIfActiveFn
	rerenderSingboxIfActiveFn = func(*Server, bridge.NodeList) error { return assertErr("rerender failed") }
	t.Cleanup(func() { rerenderSingboxIfActiveFn = oldRerender })

	form := url.Values{CSRFField(): {"token"}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/1/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	req = withRouteID(req, "1")
	rec := httptest.NewRecorder()

	srv.handleBridgeDeleteNode(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	got := readNodeList(t, nodePath)
	if len(got.Nodes) != 1 || got.Nodes[0].ID != 1 {
		t.Fatalf("delete was not rolled back: %+v", got.Nodes)
	}
}

func TestHandleBridgeSetStrategyRollsBackNodeFileOnRerenderFailure(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	writeNodeList(t, nodePath, bridge.NodeList{
		Strategy: "urltest",
		Nodes: []bridge.Node{
			{ID: 1, Tag: "a", Host: "a.example", Port: 443, Enabled: true},
			{ID: 2, Tag: "b", Host: "b.example", Port: 443, Enabled: true},
		},
	})
	srv := newBridgeTestServer(t, nodePath)
	sessionCookie := addAuthSession(t, srv)

	oldRerender := rerenderSingboxIfActiveFn
	rerenderSingboxIfActiveFn = func(*Server, bridge.NodeList) error { return assertErr("rerender failed") }
	t.Cleanup(func() { rerenderSingboxIfActiveFn = oldRerender })

	form := url.Values{CSRFField(): {"token"}, "strategy": {"roundrobin"}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/strategy", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()

	srv.handleBridgeSetStrategy(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	got := readNodeList(t, nodePath)
	if got.Strategy != "urltest" {
		t.Fatalf("strategy was not rolled back: %+v", got)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestHandleBridgeEnablePersistsBridgeModeInDB(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	teleproxyPath := filepath.Join(dir, "teleproxy.toml")
	if err := os.WriteFile(teleproxyPath, []byte("port = 443\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newBridgeTestServer(t, nodePath)
	srv.BridgeCfg.Paths.TeleproxyTOML = teleproxyPath
	srv.BridgeCfg.Paths.SingboxService = filepath.Join(dir, "sing-box.service")
	srv.BridgeCfg.Paths.SingboxBin = filepath.Join(dir, "sing-box")
	exec := newBridgeEnableExec()
	srv.BridgeExec = exec
	sessionCookie := addAuthSession(t, srv)

	form := url.Values{CSRFField(): {"token"}, "vless_url": {pe4enk0VLESSURL}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/enable", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()

	srv.handleBridgeEnable(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303, body: %s", rec.Code, rec.Body.String())
	}

	got := srv.DB.GetSetting("bridge_mode", "")
	if got != "bridge" {
		t.Errorf("bridge_mode DB setting = %q, want \"bridge\" after enable", got)
	}
}

func TestHandleBridgeDisablePersistsSingleModeInDB(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	teleproxyPath := filepath.Join(dir, "teleproxy.toml")
	if err := os.WriteFile(teleproxyPath, []byte("socks5 = \"socks5://127.0.0.1:1080\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newBridgeTestServer(t, nodePath)
	srv.BridgeCfg.Paths.TeleproxyTOML = teleproxyPath
	srv.BridgeCfg.Paths.SingboxService = filepath.Join(dir, "sing-box.service")
	srv.BridgeCfg.Paths.SingboxBin = filepath.Join(dir, "sing-box")
	exec := newBridgeEnableExec()
	srv.BridgeExec = exec
	sessionCookie := addAuthSession(t, srv)

	// Pre-set bridge_mode=bridge in DB to simulate active bridge.
	if err := srv.DB.SetSetting("bridge_mode", "bridge"); err != nil {
		t.Fatalf("set bridge_mode: %v", err)
	}

	form := url.Values{CSRFField(): {"token"}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/disable", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()

	srv.handleBridgeDisable(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303, body: %s", rec.Code, rec.Body.String())
	}

	got := srv.DB.GetSetting("bridge_mode", "")
	if got != "single" {
		t.Errorf("bridge_mode DB setting = %q, want \"single\" after disable", got)
	}
}

func TestDeleteLastActiveBridgeNodeDisablesBridgeMode(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	teleproxyPath := filepath.Join(dir, "teleproxy.toml")
	writeNodeList(t, nodePath, bridge.NodeList{
		Nodes: []bridge.Node{{ID: 1, Tag: "only-node", Host: "host", Port: 443, Enabled: true}},
	})
	if err := os.WriteFile(teleproxyPath, []byte("socks5 = \"127.0.0.1:1080\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newBridgeTestServer(t, nodePath)
	srv.BridgeCfg.Paths.TeleproxyTOML = teleproxyPath
	srv.SingboxActive = func() bool { return true }
	exec := &bridgeDeleteLastExec{}
	srv.BridgeExec = exec
	sessionCookie := addAuthSession(t, srv)

	form := url.Values{CSRFField(): {"token"}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/1/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	req = withRouteID(req, "1")
	rec := httptest.NewRecorder()

	srv.handleBridgeDeleteNode(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303, body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/p-example/bridge" {
		t.Fatalf("Location = %q, want /p-example/bridge", got)
	}

	got := readNodeList(t, nodePath)
	if len(got.Nodes) != 0 {
		t.Fatalf("last node was not deleted: %+v", got.Nodes)
	}
	if !exec.stoppedSingbox || !exec.disabledSingbox || !exec.reloadedTeleproxy {
		t.Fatalf("bridge was not disabled, exec state: %+v", exec)
	}
	if strings.Contains(string(exec.teleproxyWrite), "socks5") || strings.Contains(string(exec.teleproxyWrite), "127.0.0.1:1080") {
		t.Fatalf("teleproxy config still routes through sing-box after deleting last node:\n%s", exec.teleproxyWrite)
	}
}

type bridgeDeleteLastExec struct {
	teleproxyWrite    []byte
	stoppedSingbox    bool
	disabledSingbox   bool
	reloadedTeleproxy bool
}

func (e *bridgeDeleteLastExec) WriteFile(_ string, data []byte, _ os.FileMode) error {
	e.teleproxyWrite = append([]byte(nil), data...)
	return nil
}
func (e *bridgeDeleteLastExec) Download(_, _, _ string) error { return nil }
func (e *bridgeDeleteLastExec) EnableService(_ string) error  { return nil }
func (e *bridgeDeleteLastExec) StartService(_ string) error   { return nil }
func (e *bridgeDeleteLastExec) StopService(name string) error {
	if name == "sing-box.service" {
		e.stoppedSingbox = true
	}
	return nil
}
func (e *bridgeDeleteLastExec) DisableService(name string) error {
	if name == "sing-box.service" {
		e.disabledSingbox = true
	}
	return nil
}
func (e *bridgeDeleteLastExec) ReloadService(name string) error {
	if name == "teleproxy.service" {
		e.reloadedTeleproxy = true
	}
	return nil
}
func (e *bridgeDeleteLastExec) ServiceActive(name string) (bool, error) {
	return name == "teleproxy.service", nil
}

func TestDeleteLastActiveBridgeNodeAllowedWhenSingboxInactive(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	writeNodeList(t, nodePath, bridge.NodeList{
		Nodes: []bridge.Node{{ID: 1, Tag: "only-node", Host: "host", Port: 443, Enabled: true}},
	})
	srv := newBridgeTestServer(t, nodePath)
	srv.SingboxActive = func() bool { return false } // Stub sing-box as inactive
	sessionCookie := addAuthSession(t, srv)

	form := url.Values{CSRFField(): {"token"}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/1/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	req = withRouteID(req, "1")
	rec := httptest.NewRecorder()

	srv.handleBridgeDeleteNode(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}

	// Verify the node WAS deleted (since sing-box is not active).
	got := readNodeList(t, nodePath)
	if len(got.Nodes) != 0 {
		t.Fatalf("node was not deleted even though sing-box is inactive: %+v", got.Nodes)
	}
}

func TestDeleteLastActiveBridgeNodeAllowedWhenMultipleActive(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	writeNodeList(t, nodePath, bridge.NodeList{
		Nodes: []bridge.Node{
			{ID: 1, Tag: "node1", Host: "host1", Port: 443, Enabled: true},
			{ID: 2, Tag: "node2", Host: "host2", Port: 443, Enabled: true},
		},
	})
	srv := newBridgeTestServer(t, nodePath)
	srv.SingboxActive = func() bool { return true } // Stub sing-box as active
	sessionCookie := addAuthSession(t, srv)

	form := url.Values{CSRFField(): {"token"}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/1/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	req = withRouteID(req, "1")
	rec := httptest.NewRecorder()

	srv.handleBridgeDeleteNode(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}

	// Verify the first node WAS deleted (still have one active).
	got := readNodeList(t, nodePath)
	if len(got.Nodes) != 1 || got.Nodes[0].ID != 2 {
		t.Fatalf("deletion failed when there are multiple nodes: %+v", got.Nodes)
	}
}

func TestHandleBridgeToggleLastNodeDisablesBridge(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	teleproxyPath := filepath.Join(dir, "teleproxy.toml")
	writeNodeList(t, nodePath, bridge.NodeList{
		Nodes: []bridge.Node{{ID: 1, Tag: "only-node", Host: "host", Port: 443, Enabled: true}},
	})
	if err := os.WriteFile(teleproxyPath, []byte("socks5 = \"127.0.0.1:1080\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newBridgeTestServer(t, nodePath)
	srv.BridgeCfg.Paths.TeleproxyTOML = teleproxyPath
	srv.SingboxActive = func() bool { return true }
	exec := &bridgeDeleteLastExec{}
	srv.BridgeExec = exec
	sessionCookie := addAuthSession(t, srv)

	form := url.Values{CSRFField(): {"token"}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/1/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	req = withRouteID(req, "1")
	rec := httptest.NewRecorder()

	srv.handleBridgeToggleNode(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303, body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/p-example/bridge" {
		t.Fatalf("Location = %q, want /p-example/bridge", got)
	}

	// Node should still exist on disk but be disabled.
	got := readNodeList(t, nodePath)
	if len(got.Nodes) != 1 || got.Nodes[0].Enabled {
		t.Fatalf("node list after toggle = %+v, want 1 disabled node", got.Nodes)
	}
	// Bridge should have been disabled: sing-box stopped/disabled, teleproxy reloaded in Single mode.
	if !exec.stoppedSingbox || !exec.disabledSingbox || !exec.reloadedTeleproxy {
		t.Fatalf("bridge was not disabled after toggling last active node, exec state: %+v", exec)
	}
	if strings.Contains(string(exec.teleproxyWrite), "socks5") {
		t.Fatalf("teleproxy config still routes through sing-box after toggling off last node:\n%s", exec.teleproxyWrite)
	}
}

// TestHandleBridgeAddNodeURLRollsBackNodeFileOnRerenderFailure verifies that
// adding a node by share URL while sing-box is active restores the node file
// when the sing-box config re-render fails.
func TestHandleBridgeAddNodeURLRollsBackNodeFileOnRerenderFailure(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	writeNodeList(t, nodePath, bridge.NodeList{
		Nodes: []bridge.Node{{ID: 1, Tag: "old", Host: "old.example", Port: 443, Enabled: true}},
	})
	srv := newBridgeTestServer(t, nodePath)
	sessionCookie := addAuthSession(t, srv)
	srv.SingboxActive = func() bool { return true }
	srv.SingboxInstalled = func() bool { return true }

	oldRerender := rerenderSingboxIfActiveFn
	rerenderSingboxIfActiveFn = func(*Server, bridge.NodeList) error { return assertErr("rerender failed") }
	t.Cleanup(func() { rerenderSingboxIfActiveFn = oldRerender })

	form := url.Values{CSRFField(): {"token"}, "share_url": {pe4enk0VLESSURL}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()

	srv.handleBridgeAddNode(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	got := readNodeList(t, nodePath)
	if len(got.Nodes) != 1 || got.Nodes[0].Tag != "old" {
		t.Fatalf("node file was not rolled back: %+v", got.Nodes)
	}
}

// TestHandleBridgeAddNodeManualRollsBackNodeFileOnRerenderFailure is the
// manual-fields variant of the rollback test above.
func TestHandleBridgeAddNodeManualRollsBackNodeFileOnRerenderFailure(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	writeNodeList(t, nodePath, bridge.NodeList{
		Nodes: []bridge.Node{{ID: 1, Tag: "old", Host: "old.example", Port: 443, Enabled: true}},
	})
	srv := newBridgeTestServer(t, nodePath)
	sessionCookie := addAuthSession(t, srv)
	srv.SingboxActive = func() bool { return true }
	srv.SingboxInstalled = func() bool { return true }

	oldRerender := rerenderSingboxIfActiveFn
	rerenderSingboxIfActiveFn = func(*Server, bridge.NodeList) error { return assertErr("rerender failed") }
	t.Cleanup(func() { rerenderSingboxIfActiveFn = oldRerender })

	form := url.Values{
		CSRFField():  {"token"},
		"protocol":   {"vless-reality"},
		"tag":        {"new"},
		"host":       {"new.example"},
		"port":       {"443"},
		"uuid":       {"0498a3ad-01c8-4cfe-8317-8793b5e9dfad"},
		"sni":        {"www.example.com"},
		"public_key": {"TBuzyFsSS8dSzrL2O7lOyeDvBrcucEpSmipfYY5tMB0"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/bridge/nodes/add-manual", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "token"})
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()

	srv.handleBridgeAddNodeManual(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	got := readNodeList(t, nodePath)
	if len(got.Nodes) != 1 || got.Nodes[0].Tag != "old" {
		t.Fatalf("node file was not rolled back: %+v", got.Nodes)
	}
}
