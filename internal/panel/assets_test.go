package panel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

func TestPanelAssetsServedUnderPanelPath(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/assets/panel.css", nil)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/panel.css status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Fatalf("Content-Type = %q, want css", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=3600" {
		t.Fatalf("Cache-Control = %q, want %q", cc, "public, max-age=3600")
	}
}

func TestPanelAssetsVendoredUseImmutableCacheHeaders(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/assets/vendor/htmx-2.0.10.min.js", nil)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/vendor/htmx-2.0.10.min.js status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want %q", cc, "public, max-age=31536000, immutable")
	}
}

func TestPanelAssetsDoNotServeOutsidePanelPath(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/assets/panel.css", nil)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /assets/panel.css status = %d, want 404", rec.Code)
	}
}

func TestSecurityHeadersUseLocalOnlyAssets(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard", nil)
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	for _, forbidden := range []string{"https://", "http://", "cdn.jsdelivr", "'unsafe-inline'"} {
		if strings.Contains(csp, forbidden) {
			t.Fatalf("CSP %q contains forbidden token %q", csp, forbidden)
		}
	}
	for _, required := range []string{"default-src 'self'", "script-src 'self'", "style-src 'self'", "connect-src 'self'"} {
		if !strings.Contains(csp, required) {
			t.Fatalf("CSP %q missing %q", csp, required)
		}
	}
}

func TestLegacyUsersPageKeepsCompatCSP(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/users", nil)
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /p-example/users status = %d, want 200", rec.Code)
	}

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "'unsafe-inline'") {
		t.Fatalf("legacy users page CSP = %q, want temporary inline compatibility", csp)
	}
}

func TestPanelAssetsRejectPathTraversal(t *testing.T) {
	s := newDashboardTestServer(t)

	tests := []string{
		"/p-example/assets/../server.go",
		"/p-example/assets/%2e%2e/server.go",
		"/p-example/assets/vendor/../panel.css",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			s.Handler().ServeHTTP(rec, req)

			if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "package panel") {
				t.Fatalf("GET %s unexpectedly exposed Go source", path)
			}
		})
	}
}

func newDashboardTestServer(t *testing.T) *Server {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &Server{DB: d, PanelPath: "/p-example/", RateLimiter: NewRateLimiter(), Secure: false}
}

func authCookieForTest(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	hash, err := HashPassword("correcthorsebatterystaple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT OR REPLACE INTO admin(id, login, password_hash) VALUES(1,'admin',?)`, hash); err != nil {
		t.Fatal(err)
	}
	sessionID := "test-session"
	now := time.Now().UTC()
	if _, err := s.DB.Exec(`INSERT OR REPLACE INTO sessions(id, admin_id, expires_at, last_seen_at, ip) VALUES(?,?,?,?,?)`, sessionID, 1, now.Add(time.Hour).Format(time.RFC3339), now.Format(time.RFC3339), "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookieName, Value: sessionID}
}
