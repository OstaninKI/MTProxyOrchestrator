package panel_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
)

func TestProxySettingsPageRequiresAuth(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/p-example/settings/proxy", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("GET /settings/proxy unauthenticated: want 303, got %d", w.Code)
	}
}

func TestAdminPasswordChangeRequiresAuth(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/p-example/settings/admin-password", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("GET /settings/admin-password unauthenticated: want 303, got %d", w.Code)
	}
}

func TestSystemSettingsPageRequiresAuth(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/p-example/settings/system", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("GET /settings/system unauthenticated: want 303, got %d", w.Code)
	}
}

func TestProxySettingsPostRequiresCSRF(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	form := url.Values{"mask_host": {"www.google.com"}, "mtproto_port": {"443"}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/proxy", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	// no CSRF cookie added
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 without CSRF, got %d", w.Code)
	}
}

func TestProxySettingsRejectsInvalidPort(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	form := url.Values{
		"_csrf":        {"tok"},
		"mask_host":    {"www.google.com"},
		"mtproto_port": {"99999"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/proxy", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "tok"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with error page, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "port") {
		t.Fatalf("want port error in body, got: %s", w.Body.String())
	}
}

func TestProxySettingsRejectsEmptyMaskHost(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	form := url.Values{
		"_csrf":        {"tok"},
		"mask_host":    {""},
		"mtproto_port": {"443"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/proxy", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "tok"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with error page, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "mask host") {
		t.Fatalf("want mask host error in body, got: %s", w.Body.String())
	}
}

func TestProxySettingsSavesToDB(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	orig := panel.WriteAndReloadHook
	panel.WriteAndReloadHook = func(path string, data []byte) error { return nil }
	defer func() { panel.WriteAndReloadHook = orig }()

	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	form := url.Values{
		"_csrf":        {"tok"},
		"mask_host":    {"www.apple.com"},
		"mtproto_port": {"8443"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/proxy", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "tok"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body: %s", w.Code, w.Body.String())
	}
	if got := srv.DB.GetSetting("mask_host", ""); got != "www.apple.com" {
		t.Errorf("mask_host in DB = %q, want www.apple.com", got)
	}
	if got := srv.DB.GetSetting("mtproto_port", ""); got != "8443" {
		t.Errorf("mtproto_port in DB = %q, want 8443", got)
	}
}

func TestAdminPasswordChangeRejectsTooShort(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	form := url.Values{
		"_csrf":            {"tok"},
		"current_password": {"correcthorsebatterystaple"},
		"new_password":     {"short1"},
		"confirm_password": {"short1"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/admin-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "tok"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with error, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "16") {
		t.Fatalf("want length error (16) in body, got: %s", w.Body.String())
	}
}

func TestAdminPasswordChangeMismatch(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	form := url.Values{
		"_csrf":            {"tok"},
		"current_password": {"correcthorsebatterystaple"},
		"new_password":     {"newpassword12345678"},
		"confirm_password": {"differentpassword12345"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/admin-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "tok"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with error, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "match") {
		t.Fatalf("want mismatch error in body, got: %s", w.Body.String())
	}
}

func TestAdminPasswordChangeWrongCurrentPassword(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	form := url.Values{
		"_csrf":            {"tok"},
		"current_password": {"wrongcurrentpassword"},
		"new_password":     {"newpassword12345678"},
		"confirm_password": {"newpassword12345678"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/admin-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "tok"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with error, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "current password") && !strings.Contains(body, "incorrect") {
		t.Fatalf("want current-password error in body, got: %s", body)
	}
}

func TestAdminPasswordChangeSuccess(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	form := url.Values{
		"_csrf":            {"tok"},
		"current_password": {"correcthorsebatterystaple"},
		"new_password":     {"newPassword1234abcd"},
		"confirm_password": {"newPassword1234abcd"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/admin-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "tok"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "success") && !strings.Contains(body, "changed") {
		t.Fatalf("want success message, got: %s", body)
	}

	// Old session cookie must be invalidated — authenticated request must redirect to login.
	dashReq := httptest.NewRequest(http.MethodGet, "/p-example/dashboard", nil)
	dashReq.AddCookie(sessionCookie)
	dashW := httptest.NewRecorder()
	h.ServeHTTP(dashW, dashReq)
	if dashW.Code != http.StatusSeeOther {
		t.Fatalf("old session must be invalidated after password change, got %d", dashW.Code)
	}

	// Old password must not work.
	w2 := postLoginForm(h, "admin", "correcthorsebatterystaple")
	if w2.Code == http.StatusSeeOther {
		t.Fatal("old password must not work after change")
	}

	// New password must work.
	w3 := postLoginForm(h, "admin", "newPassword1234abcd")
	if w3.Code != http.StatusSeeOther {
		t.Fatalf("new password must work, got %d", w3.Code)
	}
}

func TestAdminPasswordChangeNoRawPasswordInAudit(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	form := url.Values{
		"_csrf":            {"tok"},
		"current_password": {"correcthorsebatterystaple"},
		"new_password":     {"newPassword1234abcd"},
		"confirm_password": {"newPassword1234abcd"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/admin-password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "tok"})
	h.ServeHTTP(httptest.NewRecorder(), req)

	rows := auditRows(t, srv.DB)
	for _, row := range rows {
		if strings.Contains(row.detail, "correcthorsebatterystaple") ||
			strings.Contains(row.detail, "newPassword1234abcd") {
			t.Fatalf("audit row must not contain raw password: %+v", row)
		}
	}
}

func TestSystemSettingsRejectsInvalidPanelPath(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	for _, badPath := range []string{"noslash", "/", "/p-/"} {
		form := url.Values{
			"_csrf":             {"tok"},
			"panel_path":        {badPath},
			"log_level":         {"info"},
			"retention_minutes": {"7"},
			"retention_hourly":  {"30"},
		}
		req := httptest.NewRequest(http.MethodPost, "/p-example/settings/system", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(sessionCookie)
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "tok"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("bad path %q: want 200 with error, got %d", badPath, w.Code)
		}
		if !strings.Contains(w.Body.String(), "path") {
			t.Fatalf("bad path %q: want path error in body, got: %s", badPath, w.Body.String())
		}
	}
}

func TestSystemSettingsSavesToDB(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	sessionCookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	form := url.Values{
		"_csrf":             {"tok"},
		"panel_path":        {"/p-newpath/"},
		"log_level":         {"warn"},
		"retention_minutes": {"14"},
		"retention_hourly":  {"60"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/system", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sessionCookie)
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "tok"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body: %s", w.Code, w.Body.String())
	}
	if got := srv.DB.GetSetting("panel_path", ""); got != "/p-newpath/" {
		t.Errorf("panel_path = %q, want /p-newpath/", got)
	}
	if got := srv.DB.GetSetting("log_level", ""); got != "warn" {
		t.Errorf("log_level = %q, want warn", got)
	}
	if got := srv.DB.GetSetting("retention_minutes_days", ""); got != "14" {
		t.Errorf("retention_minutes_days = %q, want 14", got)
	}
	if got := srv.DB.GetSetting("retention_hourly_days", ""); got != "60" {
		t.Errorf("retention_hourly_days = %q, want 60", got)
	}
}
