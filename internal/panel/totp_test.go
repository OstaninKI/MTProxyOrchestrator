package panel_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/totp"
	pquerna "github.com/pquerna/otp/totp"
)

func TestLoginWithTOTPRequiresSecondFactor(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	secret, _, _ := totp.GenerateSecret("admin")
	if _, err := srv.DB.Exec(`UPDATE admin SET totp_secret=?, totp_enabled=1 WHERE id=1`, secret); err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	w := postLoginForm(h, "admin", "correcthorsebatterystaple")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303 to totp page, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasSuffix(loc, "/totp/verify") {
		t.Fatalf("want redirect to /totp/verify, got %q", loc)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			t.Fatal("session_id must not be issued before TOTP")
		}
	}

	var pending *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "pending_totp" {
			pending = c
		}
	}
	if pending == nil || pending.Value == "" {
		t.Fatal("pending_totp cookie missing")
	}

	code, err := pquerna.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	w2 := postTOTPVerify(h, pending.Value, code)
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("totp verify: want 303, got %d body: %s", w2.Code, w2.Body.String())
	}
	gotSession := false
	for _, c := range w2.Result().Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			gotSession = true
		}
	}
	if !gotSession {
		t.Fatal("session_id cookie expected after successful totp verify")
	}
}

func TestLoginWithTOTPWrongCode(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	secret, _, _ := totp.GenerateSecret("admin")
	if _, err := srv.DB.Exec(`UPDATE admin SET totp_secret=?, totp_enabled=1 WHERE id=1`, secret); err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	w := postLoginForm(h, "admin", "correcthorsebatterystaple")
	var pending *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "pending_totp" {
			pending = c
		}
	}
	if pending == nil {
		t.Fatal("missing pending cookie")
	}
	w2 := postTOTPVerify(h, pending.Value, "000000")
	if w2.Code != http.StatusOK {
		t.Fatalf("want 200 with error, got %d", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "Invalid code") {
		t.Errorf("expected error in body, got: %s", w2.Body.String())
	}
}

func TestLoginWithTOTPRecoveryCode(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	secret, _, _ := totp.GenerateSecret("admin")
	plain, hashes, _ := totp.GenerateRecoveryCodes(2)
	enc, _ := totp.EncodeRecoveryHashes(hashes)
	if _, err := srv.DB.Exec(`UPDATE admin SET totp_secret=?, totp_enabled=1, totp_recovery_codes=? WHERE id=1`, secret, enc); err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	w := postLoginForm(h, "admin", "correcthorsebatterystaple")
	var pending *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "pending_totp" {
			pending = c
		}
	}
	w2 := postTOTPVerify(h, pending.Value, plain[0])
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("recovery login: want 303, got %d", w2.Code)
	}

	w3 := postLoginForm(h, "admin", "correcthorsebatterystaple")
	var pend2 *http.Cookie
	for _, c := range w3.Result().Cookies() {
		if c.Name == "pending_totp" {
			pend2 = c
		}
	}
	w4 := postTOTPVerify(h, pend2.Value, plain[0])
	if w4.Code != http.StatusOK {
		t.Fatalf("recovery reuse should fail (200 with error), got %d", w4.Code)
	}
}

func TestLoginWithoutTOTPStillWorks(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	h := srv.Handler()
	w := postLoginForm(h, "admin", "correcthorsebatterystaple")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/p-example/" {
		t.Fatalf("want redirect to dashboard, got %q", loc)
	}
}

func TestSettingsTOTPDisableRequiresValidCode(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	secret, _, _ := totp.GenerateSecret("admin")
	if _, err := srv.DB.Exec(`UPDATE admin SET totp_secret=?, totp_enabled=1 WHERE id=1`, secret); err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	// Login with valid code.
	w := postLoginForm(h, "admin", "correcthorsebatterystaple")
	var pending *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "pending_totp" {
			pending = c
		}
	}
	code, _ := pquerna.GenerateCode(secret, time.Now())
	w2 := postTOTPVerify(h, pending.Value, code)
	var session *http.Cookie
	for _, c := range w2.Result().Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("login failed")
	}

	// Wrong code → 200 with error, totp_enabled stays 1.
	form := url.Values{"_csrf": {"x"}, "code": {"000000"}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/settings/totp/disable", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "x"})
	req.AddCookie(session)
	wd := httptest.NewRecorder()
	h.ServeHTTP(wd, req)
	if wd.Code != http.StatusOK {
		t.Fatalf("disable bad code: want 200, got %d", wd.Code)
	}
	var enabled int
	if err := srv.DB.QueryRow(`SELECT totp_enabled FROM admin WHERE id=1`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Error("totp must remain enabled after wrong code")
	}
}

// postTOTPVerify submits the code with a pending_totp cookie and CSRF.
func postTOTPVerify(h http.Handler, pendingID, code string) *httptest.ResponseRecorder {
	csrfToken := "test-csrf-token"
	form := url.Values{
		"_csrf": {csrfToken},
		"code":  {code},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/totp/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
	req.AddCookie(&http.Cookie{Name: "pending_totp", Value: pendingID})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
