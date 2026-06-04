package panel_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestSecurityHeadersPresent(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	for _, path := range []string{"/p-example/login", "/p-example/health"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("GET %s: X-Frame-Options = %q, want DENY", path, got)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("GET %s: X-Content-Type-Options = %q, want nosniff", path, got)
		}
	}

	// Routes outside the panel path must NOT expose the security headers
	// (stealth: don't leak server identity via headers).
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("X-Frame-Options"); got != "" {
		t.Errorf("GET /health (outside panel): X-Frame-Options should be absent, got %q", got)
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

func TestHealthEndpointUnderPanelPath(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	// /health outside panel path must return 404 (no server fingerprint).
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /health at root: want 404, got %d", w.Code)
	}

	// /health under panel path must return 204.
	r2 := httptest.NewRequest(http.MethodGet, "/p-example/health", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("GET /p-example/health: want 204, got %d", w2.Code)
	}
	if got := w2.Header().Get("Content-Security-Policy"); got != "" {
		t.Fatalf("GET /p-example/health: Content-Security-Policy = %q, want empty", got)
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

func TestLoginPageUsesLocalCSSWithoutInlineStyle(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/p-example/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /p-example/login: want 200, got %d", w.Code)
	}

	html := w.Body.String()
	if !strings.Contains(html, `href="/p-example/assets/panel.css`) {
		t.Fatalf("login page should load local CSS asset, got:\n%s", html)
	}
	for _, want := range []string{
		`class="login-page"`,
		`class="app login-app"`,
		`class="login-shell"`,
		`class="card login-card"`,
		`name="_csrf" value="`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("login page missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<style>") {
		t.Fatalf("login page must not use inline style blocks")
	}

	csp := w.Header().Get("Content-Security-Policy")
	for _, forbidden := range []string{"'unsafe-inline'", "wss:"} {
		if strings.Contains(csp, forbidden) {
			t.Fatalf("login CSP %q contains forbidden token %q", csp, forbidden)
		}
	}
	for _, required := range []string{"default-src 'self'", "script-src 'self'", "style-src 'self'", "connect-src 'self'"} {
		if !strings.Contains(csp, required) {
			t.Fatalf("login CSP %q missing %q", csp, required)
		}
	}
}

func TestPanelPathWithoutTrailingSlashServesLoginAssets(t *testing.T) {
	srv := newTestServer(t, "/p-example")
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/p-example/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /p-example/login: want 200, got %d", w.Code)
	}
	html := w.Body.String()
	if !strings.Contains(html, `href="/p-example/assets/panel.css`) {
		t.Fatalf("login page missing normalized CSS path:\n%s", html)
	}
	if strings.Contains(html, "/p-exampleassets/") {
		t.Fatalf("login page contains malformed asset path:\n%s", html)
	}
}

func TestLogsPageUsesPanelAssetScriptAndLegacyCSP(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	req := httptest.NewRequest(http.MethodGet, "/p-example/logs", nil)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /p-example/logs: want 200, got %d body: %s", w.Code, w.Body.String())
	}

	html := w.Body.String()
	for _, want := range []string{
		`href="/p-example/assets/panel.css`,
		`src="/p-example/assets/panel.js`,
		`data-logs-page`,
		`data-panel-path="/p-example"`,
		`data-logs-role="download"`,
		`action="/p-example/logout"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("logs page missing %q:\n%s", want, html)
		}
	}
	for _, forbidden := range []string{"new WebSocket(", "buildWsURL", "<style>"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("logs page should not inline logs behavior token %q:\n%s", forbidden, html)
		}
	}

	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"'unsafe-inline'", "connect-src 'self' wss:"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("logs page CSP %q missing legacy token %q", csp, want)
		}
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("logs page Cache-Control = %q, want no-store", got)
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
		{name: "settings certificate renew", method: http.MethodPost, path: "/p-example/settings/certificates/renew"},
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

func TestCertificateRenewRequiresCSRF(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	srv.SettingsCfg = &panel.SettingsConfig{
		CertDir:   t.TempDir(),
		Domain:    "proxy.example.com",
		ACMEEmail: "admin@example.com",
	}
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/certificates/renew", nil)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 without CSRF, got %d body: %s", w.Code, w.Body.String())
	}
}

func TestSettingsCertificatesPageUsesNoStore(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	srv.SettingsCfg = &panel.SettingsConfig{
		CertDir:  t.TempDir(),
		Domain:   "proxy.example.com",
		ServerIP: "203.0.113.10",
	}
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	req := httptest.NewRequest(http.MethodGet, "/p-example/settings/certificates", nil)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "'unsafe-inline'") || strings.Contains(csp, "wss:") {
		t.Fatalf("certificates CSP = %q, want strict CSP without inline or wss", csp)
	}
	if !strings.Contains(w.Body.String(), `name="_csrf" value="`) {
		t.Fatalf("certificates page must render logout CSRF server-side:\n%s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `style="`) {
		t.Fatalf("certificates page must not render inline styles under strict CSP:\n%s", w.Body.String())
	}
	for _, want := range []string{
		`class="grid-12"`,
		`Active certificate`,
		`Renewal settings`,
		`class="cert-pair"`,
	} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatalf("certificates page missing %q:\n%s", want, w.Body.String())
		}
	}
}

func TestSettingsStubsPagesRenderSummaryCards(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	for _, tc := range []struct {
		path string
		want []string
	}{
		{
			path: "/p-example/settings/stubs",
			want: []string{
				`class="grid-12"`,
				`Upload custom template`,
				`Stub configuration`,
				`Template library`,
			},
		},
		{
			path: "/p-example/settings/stubs/remote",
			want: []string{
				`class="card"`,
				`learning-zone/website-templates`,
				`Remote catalog`,
				`Template library`,
			},
		},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.AddCookie(sessionCookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: want 200, got %d body: %s", tc.path, w.Code, w.Body.String())
		}
		body := w.Body.String()
		if strings.Contains(body, `style="`) {
			t.Fatalf("%s must not render inline styles:\n%s", tc.path, body)
		}
		for _, want := range tc.want {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q:\n%s", tc.path, want, body)
			}
		}
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

func TestLoginRateLimitUsesProxyRemoteAddrNotSpoofedForwardedFor(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	login, _ := seedAdmin(t, srv.DB)
	h := srv.Handler()

	for i := 0; i < 5; i++ {
		csrfToken := "test-csrf-token"
		form := url.Values{
			"_csrf":    {csrfToken},
			"login":    {login},
			"password": {"wrongpassword"},
		}
		req := httptest.NewRequest(http.MethodPost, "/p-example/login", strings.NewReader(form.Encode()))
		req.RemoteAddr = "127.0.0.1:4444"
		req.Host = "proxy.example.com"
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i+1))
		req.Header.Set("X-Real-IP", "203.0.113.10")
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})

		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was blocked too early", i+1)
		}
	}

	csrfToken := "test-csrf-token"
	form := url.Values{
		"_csrf":    {csrfToken},
		"login":    {login},
		"password": {"wrongpassword"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/login", strings.NewReader(form.Encode()))
	req.RemoteAddr = "127.0.0.1:4444"
	req.Host = "proxy.example.com"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", "198.51.100.99")
	req.Header.Set("X-Real-IP", "203.0.113.10")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("want rate limit by trusted proxy client IP, got %d", w.Code)
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

func TestIdleSessionIsRejected(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	login, password := seedAdmin(t, srv.DB)
	h := srv.Handler()

	// Log in to create a valid session
	loginResp := postLoginForm(h, login, password)
	if loginResp.Code != http.StatusSeeOther {
		t.Fatalf("login: want 303, got %d", loginResp.Code)
	}

	// Extract session cookie
	var sessionCookie *http.Cookie
	for _, c := range loginResp.Result().Cookies() {
		if c.Name == "session_id" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session_id cookie after login")
	}

	// Manually update last_seen_at to 3 hours ago to simulate idle
	old := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	_, err := srv.DB.Exec(
		`UPDATE sessions SET last_seen_at=? WHERE id=?`,
		old, sessionCookie.Value,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Try to access a protected route with the idle session
	req := httptest.NewRequest(http.MethodGet, "/p-example/", nil)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("idle session: want 303 redirect to login, got %d", w.Code)
	}
	if location := w.Header().Get("Location"); !strings.Contains(location, "/login") {
		t.Fatalf("want redirect to login, got %q", location)
	}
}
