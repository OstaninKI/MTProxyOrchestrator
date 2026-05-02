package acme_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crypto/x509"
	"encoding/pem"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/acme"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

func fixedTime(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func newManager(t *testing.T, certDir, serverIP string, dnsCheck acme.DNSChecker, now func() time.Time) acme.Manager {
	t.Helper()
	return acme.Manager{
		DB:             nil,
		CertDir:        certDir,
		AccountKeyPath: filepath.Join(certDir, "account.key"),
		ServerIP:       serverIP,
		DNSCheck:       dnsCheck,
		Now:            now,
	}
}

func TestNeedsRenewalTrue(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	m := newManager(t, t.TempDir(), "", nil, fixedTime(now))
	info := acme.CertInfo{
		ExpiresAt: now.Add(20 * 24 * time.Hour),
	}
	if !m.NeedsRenewal(info) {
		t.Fatal("expected NeedsRenewal=true for cert expiring in 20 days")
	}
}

func TestNeedsRenewalFalse(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	m := newManager(t, t.TempDir(), "", nil, fixedTime(now))
	info := acme.CertInfo{
		ExpiresAt: now.Add(60 * 24 * time.Hour),
	}
	if m.NeedsRenewal(info) {
		t.Fatal("expected NeedsRenewal=false for cert expiring in 60 days")
	}
}

func TestCheckDNSMatch(t *testing.T) {
	checker := func(domain string) ([]string, error) {
		return []string{"1.2.3.4"}, nil
	}
	m := newManager(t, t.TempDir(), "1.2.3.4", checker, time.Now)
	if err := m.CheckDNS("example.com"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestCheckDNSMismatch(t *testing.T) {
	checker := func(domain string) ([]string, error) {
		return []string{"9.9.9.9"}, nil
	}
	m := newManager(t, t.TempDir(), "1.2.3.4", checker, time.Now)
	if err := m.CheckDNS("example.com"); err == nil {
		t.Fatal("expected error for mismatched IP, got nil")
	}
}

func TestCheckDNSNotFound(t *testing.T) {
	origErr := fmt.Errorf("no such host")
	checker := func(domain string) ([]string, error) {
		return nil, origErr
	}
	m := newManager(t, t.TempDir(), "1.2.3.4", checker, time.Now)
	err := m.CheckDNS("example.com")
	if err == nil {
		t.Fatal("expected error when DNS lookup fails")
	}
}

func TestGenerateSelfSignedCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	m := newManager(t, dir, "", nil, time.Now)
	certPath, keyPath, err := m.GenerateSelfSigned("127.0.0.1")
	if err != nil {
		t.Fatalf("GenerateSelfSigned: %v", err)
	}
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("cert.pem not found: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("key.pem not found: %v", err)
	}
	// check expected paths
	expectedCert := filepath.Join(dir, "self-signed", "cert.pem")
	expectedKey := filepath.Join(dir, "self-signed", "key.pem")
	if certPath != expectedCert {
		t.Errorf("certPath = %q, want %q", certPath, expectedCert)
	}
	if keyPath != expectedKey {
		t.Errorf("keyPath = %q, want %q", keyPath, expectedKey)
	}
}

func TestGenerateSelfSignedParseable(t *testing.T) {
	dir := t.TempDir()
	m := newManager(t, dir, "", nil, time.Now)
	certPath, _, err := m.GenerateSelfSigned("127.0.0.1")
	if err != nil {
		t.Fatalf("GenerateSelfSigned: %v", err)
	}
	data, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("no PEM block in cert.pem")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
}

func TestReadCertInfo(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	m := newManager(t, dir, "", nil, fixedTime(now))
	certPath, _, err := m.GenerateSelfSigned("testhost.local")
	if err != nil {
		t.Fatalf("GenerateSelfSigned: %v", err)
	}
	info, err := m.ReadCertInfo(certPath, acme.CertModeSelfSigned)
	if err != nil {
		t.Fatalf("ReadCertInfo: %v", err)
	}
	if !info.IsValid {
		t.Error("expected IsValid=true")
	}
	if info.Mode != acme.CertModeSelfSigned {
		t.Errorf("Mode = %v, want CertModeSelfSigned", info.Mode)
	}
}

func TestDBMigrationHasCertTable(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM cert_renewals`).Scan(&count); err != nil {
		t.Fatalf("query cert_renewals: %v", err)
	}
}

func TestLoadOrCreateAccountKeyCreatesOnFirstCall(t *testing.T) {
	dir := t.TempDir()
	m := newManager(t, dir, "", nil, time.Now)
	key, err := m.LoadOrCreateAccountKey()
	if err != nil {
		t.Fatalf("LoadOrCreateAccountKey: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
	// File must exist and be mode 0600.
	info, err := os.Stat(filepath.Join(dir, "account.key"))
	if err != nil {
		t.Fatalf("account.key not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("account.key mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestLoadOrCreateAccountKeyReusesExisting(t *testing.T) {
	dir := t.TempDir()
	m := newManager(t, dir, "", nil, time.Now)

	key1, err := m.LoadOrCreateAccountKey()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	key2, err := m.LoadOrCreateAccountKey()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	// Both calls must return the same key (same X/Y coordinates).
	if key1.X.Cmp(key2.X) != 0 || key1.Y.Cmp(key2.Y) != 0 {
		t.Error("second call returned a different key; account key was not reused")
	}
}

func TestLoadOrCreateAccountKeyNoPathGeneratesInMemory(t *testing.T) {
	m := acme.Manager{CertDir: t.TempDir()} // AccountKeyPath intentionally empty
	key, err := m.LoadOrCreateAccountKey()
	if err != nil {
		t.Fatalf("LoadOrCreateAccountKey: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
}
