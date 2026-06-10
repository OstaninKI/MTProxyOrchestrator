package panel

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestLogoutSessionCookieClearedWithSameSiteStrict verifies that the cookie
// clearing the session on logout carries the same attributes as the cookie
// that created it, per the project security baseline (SameSite=Strict).
func TestLogoutSessionCookieClearedWithSameSiteStrict(t *testing.T) {
	srv := newInternalTestServer(t)

	form := url.Values{CSRFField(): {"token"}}
	req := httptest.NewRequest(http.MethodPost, "/p-example/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "token"})
	rec := httptest.NewRecorder()

	srv.handleLogout(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want 303", rec.Code)
	}
	cookie := findCookie(t, rec, sessionCookieName)
	if cookie.MaxAge >= 0 {
		t.Fatalf("session cookie MaxAge = %d, must be negative to clear", cookie.MaxAge)
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session clear cookie SameSite = %v, want Strict", cookie.SameSite)
	}
	if !cookie.HttpOnly {
		t.Fatal("session clear cookie must be HttpOnly")
	}
}

// TestClearPendingTOTPCookieSameSiteStrict verifies the pending-TOTP cookie is
// cleared with SameSite=Strict, matching how it was issued.
func TestClearPendingTOTPCookieSameSiteStrict(t *testing.T) {
	srv := newInternalTestServer(t)
	rec := httptest.NewRecorder()

	srv.clearPendingTOTP(rec, "")

	cookie := findCookie(t, rec, pendingTOTPCookieName)
	if cookie.MaxAge >= 0 {
		t.Fatalf("pending TOTP cookie MaxAge = %d, must be negative to clear", cookie.MaxAge)
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("pending TOTP clear cookie SameSite = %v, want Strict", cookie.SameSite)
	}
}

func findCookie(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("response has no %q cookie", name)
	return nil
}
