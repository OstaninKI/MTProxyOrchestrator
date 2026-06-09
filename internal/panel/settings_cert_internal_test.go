package panel

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

func newCertTestServer(t *testing.T, cfg *SettingsConfig) *Server {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &Server{DB: d, Secure: false, PanelPath: "/p-example/", SettingsCfg: cfg}
}

func postCSRF(t *testing.T, form url.Values) *http.Request {
	t.Helper()
	form.Set(CSRFField(), "tok")
	req := httptest.NewRequest(http.MethodPost, "/settings/certificates/config", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok"})
	return req
}

func TestCertRenewalConfigPersistsAll(t *testing.T) {
	srv := newCertTestServer(t, &SettingsConfig{CertDir: t.TempDir()})
	form := url.Values{"renew_days": {"21"}, "acme_provider": {"staging"}, "auto_renew": {"on"}}
	rec := httptest.NewRecorder()
	srv.handleSettingsCertRenewalConfig(rec, postCSRF(t, form))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d body: %s", rec.Code, rec.Body.String())
	}
	if got := srv.DB.GetSetting(settingCertRenewDays, ""); got != "21" {
		t.Errorf("renew_days = %q", got)
	}
	if got := srv.DB.GetSetting(settingCertACMEProvider, ""); got != "staging" {
		t.Errorf("provider = %q", got)
	}
	if got := srv.DB.GetSetting(settingCertAutoRenew, ""); got != "1" {
		t.Errorf("auto_renew = %q, want 1", got)
	}
}

func TestCertRenewalConfigAutoRenewOff(t *testing.T) {
	srv := newCertTestServer(t, &SettingsConfig{CertDir: t.TempDir()})
	// checkbox absent => off
	form := url.Values{"renew_days": {"30"}, "acme_provider": {"production"}}
	rec := httptest.NewRecorder()
	srv.handleSettingsCertRenewalConfig(rec, postCSRF(t, form))
	if got := srv.DB.GetSetting(settingCertAutoRenew, ""); got != "0" {
		t.Errorf("auto_renew = %q, want 0", got)
	}
}

func TestCertRenewalConfigRejectsBadProvider(t *testing.T) {
	srv := newCertTestServer(t, &SettingsConfig{CertDir: t.TempDir()})
	form := url.Values{"renew_days": {"30"}, "acme_provider": {"bogus"}, "auto_renew": {"on"}}
	rec := httptest.NewRecorder()
	srv.handleSettingsCertRenewalConfig(rec, postCSRF(t, form))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("want 303 redirect, got %d", rec.Code)
	}
	// provider must not be persisted
	if got := srv.DB.GetSetting(settingCertACMEProvider, "unset"); got != "unset" {
		t.Errorf("provider should be unchanged, got %q", got)
	}
}
