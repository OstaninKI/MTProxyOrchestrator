package panel_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
)

func newTestServer(t *testing.T, panelPath string) *panel.Server {
	t.Helper()
	return &panel.Server{
		DB:          newTestDB(t),
		PanelPath:   panelPath,
		RateLimiter: panel.NewRateLimiter(),
		Secure:      false,
	}
}

func TestOutsidePanelPathReturns404(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	for _, path := range []string{"/", "/login", "/admin", "/p-other/"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s: want 404, got %d", path, w.Code)
		}
	}
}

func TestHealthEndpointServedOutsidePanelPath(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("GET /health: want 204, got %d", w.Code)
	}
	if body := w.Body.String(); body != "" {
		t.Fatalf("GET /health: want empty body, got %q", body)
	}
}

func TestLoginPageServedUnderPanelPath(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/p-example/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("GET /p-example/login: want 200, got %d", w.Code)
	}
}

func TestDashboardRedirectsUnauthenticated(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/p-example/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("unauthenticated dashboard: want 303, got %d", w.Code)
	}
}

func TestProtectedBridgeAndSettingsRoutesAreMounted(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "bridge manual add", method: http.MethodPost, path: "/p-example/bridge/nodes/add-manual"},
		{name: "bridge edit form", method: http.MethodGet, path: "/p-example/bridge/nodes/1/edit"},
		{name: "bridge edit submit", method: http.MethodPost, path: "/p-example/bridge/nodes/1/edit"},
		{name: "bridge ping", method: http.MethodPost, path: "/p-example/bridge/nodes/1/ping"},
		{name: "bridge strategy", method: http.MethodPost, path: "/p-example/bridge/strategy"},
		{name: "settings stubs list", method: http.MethodGet, path: "/p-example/settings/stubs"},
		{name: "settings stubs apply", method: http.MethodPost, path: "/p-example/settings/stubs/apply"},
		{name: "settings stubs upload", method: http.MethodPost, path: "/p-example/settings/stubs/upload"},
		{name: "settings certificates", method: http.MethodGet, path: "/p-example/settings/certificates"},
		{name: "settings proxy get", method: http.MethodGet, path: "/p-example/settings/proxy"},
		{name: "settings proxy post", method: http.MethodPost, path: "/p-example/settings/proxy"},
		{name: "settings admin-password get", method: http.MethodGet, path: "/p-example/settings/admin-password"},
		{name: "settings admin-password post", method: http.MethodPost, path: "/p-example/settings/admin-password"},
		{name: "settings system get", method: http.MethodGet, path: "/p-example/settings/system"},
		{name: "settings system post", method: http.MethodPost, path: "/p-example/settings/system"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != http.StatusSeeOther {
				t.Fatalf("%s %s: want 303 to login, got %d", tc.method, tc.path, w.Code)
			}
			if location := w.Header().Get("Location"); location != "/p-example/login" {
				t.Fatalf("%s %s: want redirect to /p-example/login, got %q", tc.method, tc.path, location)
			}
		})
	}
}

// seedAdmin inserts an admin row and returns the plain-text password.
func seedAdmin(t *testing.T, d *db.DB) (login, password string) {
	t.Helper()
	login = "admin"
	password = "correcthorsebatterystaple"
	hash, err := panel.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO admin(id, login, password_hash) VALUES(1,?,?)`, login, hash); err != nil {
		t.Fatal(err)
	}
	return login, password
}

// auditRows returns all (action, target, detail) triples from audit_log.
func auditRows(t *testing.T, d *db.DB) []struct{ action, target, detail string } {
	t.Helper()
	rows, err := d.Query(`SELECT action, target, detail FROM audit_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []struct{ action, target, detail string }
	for rows.Next() {
		var row struct{ action, target, detail string }
		if err := rows.Scan(&row.action, &row.target, &row.detail); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	return out
}

// postLoginForm sends a POST /p-example/login request with CSRF.
func postLoginForm(h http.Handler, login, password string) *httptest.ResponseRecorder {
	csrfToken := "test-csrf-token"
	form := url.Values{
		"_csrf":    {csrfToken},
		"login":    {login},
		"password": {password},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestAuditLoginSuccess(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	login, password := seedAdmin(t, srv.DB)
	h := srv.Handler()

	w := postLoginForm(h, login, password)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login: want 303, got %d", w.Code)
	}

	rows := auditRows(t, srv.DB)
	if len(rows) != 1 {
		t.Fatalf("want 1 audit row, got %d: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.action != "login_success" {
		t.Errorf("action = %q, want %q", row.action, "login_success")
	}
	if row.target != login {
		t.Errorf("target = %q, want login %q", row.target, login)
	}
	if strings.Contains(row.detail, password) {
		t.Errorf("audit detail must not contain password")
	}
}

func TestAuditLoginFailedNoPassword(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	login, password := seedAdmin(t, srv.DB)
	h := srv.Handler()

	w := postLoginForm(h, login, "wrongpassword")
	if w.Code == http.StatusSeeOther {
		t.Fatalf("bad login: should not redirect")
	}

	rows := auditRows(t, srv.DB)
	if len(rows) != 1 {
		t.Fatalf("want 1 audit row, got %d: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.action != "login_failed" {
		t.Errorf("action = %q, want %q", row.action, "login_failed")
	}
	// password must never appear in any audit field
	for _, field := range []string{row.action, row.target, row.detail} {
		if strings.Contains(field, password) || strings.Contains(field, "wrongpassword") {
			t.Errorf("audit field %q must not contain a password", field)
		}
	}
}

func TestAuditRateLimitedNoPassword(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	login, password := seedAdmin(t, srv.DB)
	h := srv.Handler()

	// Exhaust the rate limit (5 failures)
	for range 5 {
		postLoginForm(h, login, "wrongpassword")
	}

	// Clear audit so far; next attempt should be blocked
	if _, err := srv.DB.Exec(`DELETE FROM audit_log`); err != nil {
		t.Fatal(err)
	}

	w := postLoginForm(h, login, "wrongpassword")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", w.Code)
	}

	rows := auditRows(t, srv.DB)
	if len(rows) != 1 {
		t.Fatalf("want 1 audit row for rate-limit block, got %d: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.action != "login_rate_limited" {
		t.Errorf("action = %q, want %q", row.action, "login_rate_limited")
	}
	for _, field := range []string{row.action, row.target, row.detail} {
		if strings.Contains(field, password) || strings.Contains(field, "wrongpassword") {
			t.Errorf("audit field %q must not contain a password", field)
		}
	}
}

func TestAuditLogoutNoSessionToken(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	login, password := seedAdmin(t, srv.DB)
	h := srv.Handler()

	// Log in to get a session cookie
	loginResp := postLoginForm(h, login, password)
	if loginResp.Code != http.StatusSeeOther {
		t.Fatalf("login: want 303, got %d", loginResp.Code)
	}

	// Extract session cookie from response
	var sessionCookie *http.Cookie
	for _, c := range loginResp.Result().Cookies() {
		if c.Name == "session_id" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session_id cookie after login")
	}

	// Clear audit rows from login
	if _, err := srv.DB.Exec(`DELETE FROM audit_log`); err != nil {
		t.Fatal(err)
	}

	// Send logout with CSRF
	csrfToken := "logout-csrf-token"
	form := url.Values{"_csrf": {csrfToken}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("logout: want 303, got %d", w.Code)
	}

	rows := auditRows(t, srv.DB)
	if len(rows) != 1 {
		t.Fatalf("want 1 audit row for logout, got %d: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.action != "logout" {
		t.Errorf("action = %q, want %q", row.action, "logout")
	}
	// Session token value must not appear in any audit field
	sessionTokenVal := sessionCookie.Value
	for _, field := range []string{row.action, row.target, row.detail} {
		if strings.Contains(field, sessionTokenVal) {
			t.Errorf("audit field %q must not contain session token", field)
		}
	}
}
