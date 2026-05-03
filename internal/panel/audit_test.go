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
