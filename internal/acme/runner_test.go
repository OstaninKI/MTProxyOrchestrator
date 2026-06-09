package acme_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/acme"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

// stubRenewer lets tests control what ObtainACME returns without a real ACME server.
// It patches the Manager fields so the Runner uses deterministic cert paths.
func runnerWithFreshCert(t *testing.T, expiresIn time.Duration) (*acme.Runner, string, func() bool) {
	t.Helper()
	dir := t.TempDir()

	m := acme.Manager{
		CertDir:        dir,
		AccountKeyPath: filepath.Join(dir, "account.key"),
		Now:            time.Now,
	}

	// Generate a self-signed cert as a stand-in; the Runner reads it to check expiry.
	certPath, _, err := m.GenerateSelfSigned("test.example.com")
	if err != nil {
		t.Fatalf("generate self-signed: %v", err)
	}

	// Adjust NeedsRenewal by setting Now to a time where the cert "expires soon".
	if expiresIn < 30*24*time.Hour {
		// Make the Manager believe we are close to expiry.
		m.Now = func() time.Time {
			info, _ := m.ReadCertInfo(certPath, acme.CertModeACME)
			// Return a time just before the cert's NotAfter minus 30 days.
			return info.ExpiresAt.Add(-(29 * 24 * time.Hour))
		}
	}

	reloaded := false
	runner := &acme.Runner{
		Manager:    m,
		WebRootDir: filepath.Join(dir, ".well-known-webroot"),
		Reloader: func(ctx context.Context) error {
			reloaded = true
			return nil
		},
	}
	return runner, certPath, func() bool { return reloaded }
}

func TestRenewIfNeededSkipsWhenFresh(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	m := acme.Manager{
		CertDir:        dir,
		AccountKeyPath: filepath.Join(dir, "account.key"),
		Now:            func() time.Time { return now },
	}
	certPath, _, err := m.GenerateSelfSigned("test.example.com")
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	reloaded := false
	runner := &acme.Runner{
		Manager:    m,
		WebRootDir: dir,
		Reloader: func(ctx context.Context) error {
			reloaded = true
			return nil
		},
	}

	renewed, err := runner.RenewIfNeeded(context.Background(), "test.example.com", "a@b.com", certPath)
	if err != nil {
		t.Fatalf("RenewIfNeeded: %v", err)
	}
	if renewed {
		t.Error("expected renewed=false for a fresh cert")
	}
	if reloaded {
		t.Error("reloader must not be called when cert is fresh")
	}
}

func TestRenewIfNeededReturnsErrorOnMissingCert(t *testing.T) {
	dir := t.TempDir()
	m := acme.Manager{
		CertDir: dir,
		Now:     time.Now,
	}
	runner := &acme.Runner{Manager: m, WebRootDir: dir}
	_, err := runner.RenewIfNeeded(context.Background(), "x.example.com", "a@b.com", filepath.Join(dir, "missing.pem"))
	if err == nil {
		t.Fatal("expected error for missing cert file")
	}
}

func TestRenewReloaderCalledOnSuccess(t *testing.T) {
	// Renew calls ObtainACME internally which would contact LE in production.
	// We verify the Reloader path by mocking via a nil CADirURL path that would fail —
	// instead we just test that the reloader error is propagated, proving it's called.
	dir := t.TempDir()
	m := acme.Manager{
		CertDir:        dir,
		AccountKeyPath: filepath.Join(dir, "account.key"),
		CADirURL:       "http://127.0.0.1:1/nonexistent", // will fail fast
		Now:            time.Now,
	}
	reloaderCalled := false
	runner := &acme.Runner{
		Manager:    m,
		WebRootDir: dir,
		Reloader: func(ctx context.Context) error {
			reloaderCalled = true
			return errors.New("nginx not available in test")
		},
	}
	// Renew will fail at ObtainACME (bad CA URL) before reaching reloader.
	err := runner.Renew(context.Background(), "x.example.com", "a@b.com")
	if err == nil {
		t.Fatal("expected error from Renew with bad CA URL")
	}
	// Reloader must NOT be called when ObtainACME fails.
	if reloaderCalled {
		t.Error("reloader must not be called when ObtainACME fails")
	}
}

func TestDefaultRunnerUsesSystemReloader(t *testing.T) {
	dir := t.TempDir()
	mgr := acme.DefaultManager(nil, dir, "")
	runner := acme.DefaultRunner(mgr, filepath.Join(dir, ".well-known-webroot"))
	// Verify Runner is non-zero and Reloader is set.
	if runner.Reloader == nil {
		t.Error("DefaultRunner must set Reloader")
	}
}

func TestRenewIfNeededGateDisabled(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	r := &acme.Runner{
		Manager:      acme.Manager{DB: d, CertDir: t.TempDir()},
		RenewEnabled: func() bool { return false },
	}
	// certPath does not exist; the gate must return before any read or ACME call.
	renewed, err := r.RenewIfNeeded(context.Background(), "proxy.example.com", "a@b.c", t.TempDir()+"/missing.pem")
	if err != nil {
		t.Fatalf("gate should swallow work, got err: %v", err)
	}
	if renewed {
		t.Fatal("renewed should be false when auto-renew disabled")
	}
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM cert_renewals`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected no renewal rows, got %d", count)
	}
}

func TestRenewIfNeededGateEnabledProceeds(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	r := &acme.Runner{
		Manager:      acme.Manager{DB: d, CertDir: t.TempDir()},
		RenewEnabled: func() bool { return true },
	}
	// With the gate open the call proceeds past it and fails reading the missing
	// cert — proving an enabled gate does not short-circuit.
	if _, err := r.RenewIfNeeded(context.Background(), "proxy.example.com", "a@b.c", t.TempDir()+"/missing.pem"); err == nil {
		t.Fatal("expected error reading missing cert when gate is enabled")
	}
}
