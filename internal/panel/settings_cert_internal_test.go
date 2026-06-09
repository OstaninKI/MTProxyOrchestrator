package panel

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func makeCertPair(t *testing.T, dnsName string, notAfter time.Time) (cert, key []byte) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: dnsName},
		DNSNames:     []string{dnsName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	cert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kb, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	key = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	return cert, key
}

func uploadReq(t *testing.T, certPEM, keyPEM []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField(CSRFField(), "tok")
	if certPEM != nil {
		fw, _ := mw.CreateFormFile("cert", "fullchain.pem")
		fw.Write(certPEM)
	}
	if keyPEM != nil {
		fw, _ := mw.CreateFormFile("key", "privkey.pem")
		fw.Write(keyPEM)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/settings/certificates/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok"})
	return req
}

func TestCertUploadHappyPath(t *testing.T) {
	dir := t.TempDir()
	srv := newCertTestServer(t, &SettingsConfig{CertDir: dir, Domain: "proxy.example.com"})
	reloadNginx = func() error { return nil }
	t.Cleanup(func() {
		reloadNginx = func() error { return nil }
	})
	cert, key := makeCertPair(t, "proxy.example.com", time.Now().Add(90*24*time.Hour))
	rec := httptest.NewRecorder()
	srv.handleSettingsCertUpload(rec, uploadReq(t, cert, key))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d body: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "proxy.example.com", "cert.pem")); err != nil {
		t.Fatalf("cert not written: %v", err)
	}
	if got := srv.DB.GetSetting(settingCertManual, ""); got != "1" {
		t.Errorf("cert_manual = %q, want 1", got)
	}
	if got := srv.DB.GetSetting(settingCertAutoRenew, ""); got != "0" {
		t.Errorf("cert_auto_renew = %q, want 0", got)
	}
}

func TestCertUploadRejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	srv := newCertTestServer(t, &SettingsConfig{CertDir: dir, Domain: "proxy.example.com"})
	cert, _ := makeCertPair(t, "proxy.example.com", time.Now().Add(90*24*time.Hour))
	_, otherKey := makeCertPair(t, "proxy.example.com", time.Now().Add(90*24*time.Hour))
	rec := httptest.NewRecorder()
	srv.handleSettingsCertUpload(rec, uploadReq(t, cert, otherKey))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("want 303 with error notice, got %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "proxy.example.com", "cert.pem")); err == nil {
		t.Fatal("cert should not be written on validation failure")
	}
	if got := srv.DB.GetSetting(settingCertManual, "unset"); got != "unset" {
		t.Errorf("cert_manual should be unchanged, got %q", got)
	}
}

func TestCertUploadRequiresDomain(t *testing.T) {
	srv := newCertTestServer(t, &SettingsConfig{CertDir: t.TempDir()}) // no domain
	cert, key := makeCertPair(t, "proxy.example.com", time.Now().Add(90*24*time.Hour))
	rec := httptest.NewRecorder()
	srv.handleSettingsCertUpload(rec, uploadReq(t, cert, key))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("want 303 with error notice, got %d", rec.Code)
	}
	if got := srv.DB.GetSetting(settingCertManual, "unset"); got != "unset" {
		t.Errorf("cert_manual should be unchanged, got %q", got)
	}
}

func TestCertManualClearResetsFlags(t *testing.T) {
	srv := newCertTestServer(t, &SettingsConfig{CertDir: t.TempDir(), Domain: "proxy.example.com"})
	srv.DB.SetSetting(settingCertManual, "1")
	srv.DB.SetSetting(settingCertAutoRenew, "0")
	form := url.Values{CSRFField(): {"tok"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/certificates/manual/clear", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok"})
	rec := httptest.NewRecorder()
	srv.handleSettingsCertManualClear(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", rec.Code)
	}
	if got := srv.DB.GetSetting(settingCertManual, ""); got != "0" {
		t.Errorf("cert_manual = %q, want 0", got)
	}
	if got := srv.DB.GetSetting(settingCertAutoRenew, ""); got != "1" {
		t.Errorf("cert_auto_renew = %q, want 1", got)
	}
}
