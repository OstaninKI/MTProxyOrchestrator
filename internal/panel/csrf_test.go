package panel_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
)

func TestCSRFTokenLength(t *testing.T) {
	tok, err := panel.NewCSRFToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 {
		t.Errorf("token length = %d, want 64", len(tok))
	}
}

func TestCSRFTokenUniqueness(t *testing.T) {
	a, err := panel.NewCSRFToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := panel.NewCSRFToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two tokens are identical")
	}
}

func TestValidateCSRFPass(t *testing.T) {
	tok, err := panel.NewCSRFToken()
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{panel.CSRFField(): {tok}}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: tok})
	if !panel.ValidateCSRF(r) {
		t.Error("valid CSRF should pass")
	}
}

func TestValidateCSRFMismatch(t *testing.T) {
	tok, err := panel.NewCSRFToken()
	if err != nil {
		t.Fatal(err)
	}
	other, err := panel.NewCSRFToken()
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{panel.CSRFField(): {other}}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: tok})
	if panel.ValidateCSRF(r) {
		t.Error("mismatched CSRF should fail")
	}
}

func TestValidateCSRFMissingCookie(t *testing.T) {
	tok, err := panel.NewCSRFToken()
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{panel.CSRFField(): {tok}}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if panel.ValidateCSRF(r) {
		t.Error("missing CSRF cookie should fail")
	}
}
