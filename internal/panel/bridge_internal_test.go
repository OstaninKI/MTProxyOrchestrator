package panel

import (
	"context"
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

func TestDeleteLastActiveBridgeNodeIsRejected(t *testing.T) {
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	writeNodeList(t, nodePath, bridge.NodeList{
		Nodes: []bridge.Node{{ID: 1, Tag: "only-node", Host: "host", Port: 443, Enabled: true}},
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
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}

	// Verify the node was NOT deleted.
	got := readNodeList(t, nodePath)
	if len(got.Nodes) != 1 || got.Nodes[0].ID != 1 {
		t.Fatalf("node was deleted despite being the last active: %+v", got.Nodes)
	}
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
