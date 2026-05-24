package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"golang.org/x/crypto/bcrypt"
)

func TestBridgeExecDefaultsToRealExecutor(t *testing.T) {
	srv := &Server{}
	got := srv.bridgeExec()
	if _, ok := got.(realBridgeExecutor); !ok {
		t.Fatalf("want realBridgeExecutor, got %T", got)
	}
}

func TestBridgeExecUsesInjectedExecutor(t *testing.T) {
	srv := &Server{BridgeExec: noopBridgeExecutor{}}
	got := srv.bridgeExec()
	if _, ok := got.(noopBridgeExecutor); !ok {
		t.Fatalf("want noopBridgeExecutor, got %T", got)
	}
}

func TestSeedDevDataCreatesAdmin(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := SeedDevData(d); err != nil {
		t.Fatalf("SeedDevData: %v", err)
	}

	var login string
	if err := d.QueryRow("SELECT login FROM admin WHERE id=1").Scan(&login); err != nil {
		t.Fatalf("admin row not found: %v", err)
	}
	if login != "admin" {
		t.Fatalf("want login=admin, got %q", login)
	}
}

func TestSeedDevDataAdminPasswordIsAdmin(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := SeedDevData(d); err != nil {
		t.Fatalf("SeedDevData: %v", err)
	}

	var hash string
	if err := d.QueryRow("SELECT password_hash FROM admin WHERE id=1").Scan(&hash); err != nil {
		t.Fatalf("admin hash not found: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("admin")); err != nil {
		t.Fatalf("password 'admin' does not match stored hash: %v", err)
	}
}

func TestSeedDevDataCreatesUsers(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := SeedDevData(d); err != nil {
		t.Fatalf("SeedDevData: %v", err)
	}

	var count int
	if err := d.QueryRow("SELECT COUNT(*) FROM users WHERE deleted_at IS NULL").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 3 {
		t.Fatalf("want >= 3 users, got %d", count)
	}
}

func TestSeedDevDataCreatesTrafficSamples(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := SeedDevData(d); err != nil {
		t.Fatalf("SeedDevData: %v", err)
	}

	var count int
	if err := d.QueryRow("SELECT COUNT(*) FROM traffic_samples").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("want traffic samples seeded, got 0 rows")
	}
}

func TestNoopBridgeExecutorAllMethodsSucceed(t *testing.T) {
	e := noopBridgeExecutor{}

	if err := e.WriteFile("/any/path", []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := e.Download("http://example.com", "abc123", "/tmp/dest"); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if err := e.EnableService("sing-box.service"); err != nil {
		t.Fatalf("EnableService: %v", err)
	}
	if err := e.StartService("sing-box.service"); err != nil {
		t.Fatalf("StartService: %v", err)
	}
	if err := e.StopService("sing-box.service"); err != nil {
		t.Fatalf("StopService: %v", err)
	}
	if err := e.DisableService("sing-box.service"); err != nil {
		t.Fatalf("DisableService: %v", err)
	}
	if err := e.ReloadService("sing-box.service"); err != nil {
		t.Fatalf("ReloadService: %v", err)
	}
	active, err := e.ServiceActive("sing-box.service")
	if err != nil {
		t.Fatalf("ServiceActive: %v", err)
	}
	if active {
		t.Fatal("ServiceActive: want false, got true")
	}
}

func TestApplyDevModeSetsExpectedFields(t *testing.T) {
	srv := &Server{Secure: true}
	ApplyDevMode(srv)

	if !srv.DevMode {
		t.Error("want DevMode=true after ApplyDevMode")
	}
	if srv.Secure {
		t.Error("want Secure=false after ApplyDevMode")
	}
	if srv.SingboxActive == nil {
		t.Fatal("want SingboxActive != nil")
	}
	if srv.SingboxActive() {
		t.Error("want SingboxActive()=false in dev mode")
	}
	if _, ok := srv.BridgeExec.(noopBridgeExecutor); !ok {
		t.Errorf("want BridgeExec=noopBridgeExecutor, got %T", srv.BridgeExec)
	}
}

func TestApplyDevModeHooksAreNoop(t *testing.T) {
	origWrite := WriteAndReloadHook
	origSync := SyncUsersJSONHook
	origNginx := reloadNginx
	origSingbox := isSingboxActive
	t.Cleanup(func() {
		WriteAndReloadHook = origWrite
		SyncUsersJSONHook = origSync
		reloadNginx = origNginx
		isSingboxActive = origSingbox
	})

	srv := &Server{}
	ApplyDevMode(srv)

	if err := WriteAndReloadHook("/nonexistent/teleproxy.toml", []byte("x")); err != nil {
		t.Fatalf("WriteAndReloadHook should be noop, got %v", err)
	}
	if err := SyncUsersJSONHook("/nonexistent/users.json", []byte("x")); err != nil {
		t.Fatalf("SyncUsersJSONHook should be noop, got %v", err)
	}
	if err := reloadNginx(); err != nil {
		t.Fatalf("reloadNginx should be noop, got %v", err)
	}
	if isSingboxActive() {
		t.Fatal("isSingboxActive should return false in dev mode")
	}
}

func TestDevModeUIRoutesRespond(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := SeedDevData(d); err != nil {
		t.Fatalf("SeedDevData: %v", err)
	}

	srv := &Server{
		DB:          d,
		PanelPath:   "/",
		RateLimiter: NewRateLimiter(),
	}

	// Save and restore package-level hooks that ApplyDevMode will override.
	origWrite := WriteAndReloadHook
	origSync := SyncUsersJSONHook
	origNginx := reloadNginx
	origSingbox := isSingboxActive
	t.Cleanup(func() {
		WriteAndReloadHook = origWrite
		SyncUsersJSONHook = origSync
		reloadNginx = origNginx
		isSingboxActive = origSingbox
	})

	ApplyDevMode(srv)

	h := srv.Handler()

	// Unauthenticated public routes must return 200 or 204.
	for _, path := range []string{"/login", "/health"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
			t.Errorf("GET %s: want 200/204, got %d", path, w.Code)
		}
	}

	// Authenticated routes must redirect to login when no session cookie is present.
	for _, path := range []string{
		"/dashboard", "/users", "/bridge", "/logs", "/audit",
		"/settings/proxy", "/settings/certificates", "/settings/stubs",
	} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusSeeOther {
			t.Errorf("GET %s (no session): want 303, got %d", path, w.Code)
		}
	}
}
