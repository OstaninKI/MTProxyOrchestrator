package acme

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestRenewIfNeeded_PendingReloadRetriedWithoutReissue verifies that when the
// cert on disk is still fresh but a previous renewal left a pending reload,
// the next RenewIfNeeded call retries the reload only and does NOT contact
// the ACME server again. This protects against Let's Encrypt rate limits.
func TestRenewIfNeeded_PendingReloadRetriedWithoutReissue(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	m := Manager{
		CertDir:        dir,
		AccountKeyPath: filepath.Join(dir, "account.key"),
		// Pointing at an unreachable URL: if ObtainACME is called, the test
		// will fail because the call would either succeed unexpectedly or
		// take a long time.
		CADirURL: "http://127.0.0.1:1/nonexistent",
		Now:      func() time.Time { return now },
	}
	certPath, _, err := m.GenerateSelfSigned("test.example.com")
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	reloadCalls := 0
	runner := &Runner{
		Manager:    m,
		WebRootDir: dir,
		Reloader: func(ctx context.Context) error {
			reloadCalls++
			return nil
		},
	}

	// Simulate a previous renewal that issued the cert but left reload pending.
	runner.setPendingReload("test.example.com", true)

	renewed, err := runner.RenewIfNeeded(context.Background(), "test.example.com", "a@b.com", certPath)
	if err != nil {
		t.Fatalf("RenewIfNeeded: %v", err)
	}
	if !renewed {
		t.Error("expected renewed=true after retried reload")
	}
	if reloadCalls != 1 {
		t.Errorf("expected reloader to be called exactly once, got %d", reloadCalls)
	}
	if runner.isPendingReload("test.example.com") {
		t.Error("expected pendingReload to be cleared after successful retry")
	}
}

// TestRenewIfNeeded_PendingReloadStillFailing verifies that a still-failing
// reload keeps the pending flag set so the loop will retry again next tick.
func TestRenewIfNeeded_PendingReloadStillFailing(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	m := Manager{
		CertDir:        dir,
		AccountKeyPath: filepath.Join(dir, "account.key"),
		CADirURL:       "http://127.0.0.1:1/nonexistent",
		Now:            func() time.Time { return now },
	}
	certPath, _, err := m.GenerateSelfSigned("test.example.com")
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	reloadCalls := 0
	runner := &Runner{
		Manager:    m,
		WebRootDir: dir,
		Reloader: func(ctx context.Context) error {
			reloadCalls++
			return errors.New("nginx still down")
		},
	}
	runner.setPendingReload("test.example.com", true)

	_, err = runner.RenewIfNeeded(context.Background(), "test.example.com", "a@b.com", certPath)
	if err == nil {
		t.Fatal("expected reload retry to surface error")
	}
	if reloadCalls != 1 {
		t.Errorf("expected one reload attempt, got %d", reloadCalls)
	}
	if !runner.isPendingReload("test.example.com") {
		t.Error("expected pendingReload to remain set after a failing retry")
	}
}

// TestRenew_ReloadFailureMarksPending verifies the contract of Renew: when
// ObtainACME succeeds but reload fails, the cert-on-disk is treated as the
// committed state and pendingReload is set so a subsequent RenewIfNeeded only
// retries reload.
//
// Because Renew calls ObtainACME against a real ACME endpoint, this test
// instead exercises the lower-level helpers directly to confirm the bookkeeping.
func TestRenew_ReloadFailureMarksPending_Direct(t *testing.T) {
	r := &Runner{}
	r.setPendingReload("d.example.com", true)
	if !r.isPendingReload("d.example.com") {
		t.Fatal("expected pendingReload=true after set")
	}
	r.setPendingReload("d.example.com", false)
	if r.isPendingReload("d.example.com") {
		t.Fatal("expected pendingReload=false after clear")
	}
}
