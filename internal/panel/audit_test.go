package panel_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditLogPageRequiresAuth(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()
	req := httptest.NewRequest("GET", "/p-example/audit", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("expected redirect, got %d", w.Code)
	}
}

func TestAuditLogPageRendersEntries(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	login, password := seedAdmin(t, srv.DB)
	h := srv.Handler()

	// Insert a test audit entry
	if _, err := srv.DB.Exec(
		`INSERT INTO audit_log(admin_id, action, target, detail, ip) VALUES(1,'login_success','admin','','1.2.3.4')`,
	); err != nil {
		t.Fatal(err)
	}

	// Log in to get session
	sessionCookie := doLogin(t, h, login, password)

	req := httptest.NewRequest("GET", "/p-example/audit", nil)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "login_success") {
		t.Error("expected action in body")
	}
}

func TestAuditLogPageRendersServerLogoutCSRF(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	login, password := seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, login, password)

	req := httptest.NewRequest("GET", "/p-example/audit", nil)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "'unsafe-inline'") || strings.Contains(csp, "wss:") {
		t.Fatalf("audit CSP = %q, want strict CSP without inline or wss", csp)
	}
	body := w.Body.String()
	if !strings.Contains(body, `action="/p-example/logout"`) {
		t.Fatalf("logout action missing panel path:\n%s", body)
	}
	if !strings.Contains(body, `name="_csrf" value="`) {
		t.Fatalf("logout form must render CSRF value server-side:\n%s", body)
	}
	if strings.Contains(body, `name="_csrf" class="js-csrf"`) {
		t.Fatalf("logout form must not rely on JS-only CSRF injection:\n%s", body)
	}
	if strings.Contains(body, `style="`) {
		t.Fatalf("audit page must not render inline styles under strict CSP:\n%s", body)
	}
	var csrfCookie bool
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "csrf_token" && cookie.Value != "" {
			csrfCookie = true
		}
	}
	if !csrfCookie {
		t.Fatal("audit page must set a CSRF cookie for the logout form")
	}
}

func TestAuditLogPageShowsNoEntriesMessage(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	login, password := seedAdmin(t, srv.DB)
	h := srv.Handler()

	// Log in to get session (will create one audit entry for login)
	sessionCookie := doLogin(t, h, login, password)

	// Now clear audit table
	if _, err := srv.DB.Exec(`DELETE FROM audit_log`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/p-example/audit", nil)
	req.AddCookie(sessionCookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "No entries") {
		t.Error("expected 'No entries' message when audit log is empty")
	}
}
