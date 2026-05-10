package update

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeDownloader records calls and optionally fails.
type fakeDownloader struct {
	err     error
	written []byte // content to write to destPath on success
}

func (f *fakeDownloader) Download(url, sha256hex, destPath string) error {
	if f.err != nil {
		return f.err
	}
	content := f.written
	if content == nil {
		content = []byte("new-binary")
	}
	return os.WriteFile(destPath, content, 0o755)
}

// fakeSVC records Restart calls.
type fakeSVC struct {
	restartCalls []string
	err          error
}

func (f *fakeSVC) Restart(svc string) error {
	f.restartCalls = append(f.restartCalls, svc)
	return f.err
}

// fakeHealth can be configured to fail once then succeed (simulating restart recovery).
type fakeHealth struct {
	failCount int
	calls     int
}

func (f *fakeHealth) Check(svc string) error {
	f.calls++
	if f.calls <= f.failCount {
		return fmt.Errorf("service %s not healthy", svc)
	}
	return nil
}

// fakeFileOps wraps real FS calls but records operations.
type fakeFileOps struct {
	OSFileOps
	chmodErr  error
	renameErr error
}

func (f *fakeFileOps) Chmod(path string, mode os.FileMode) error {
	if f.chmodErr != nil {
		return f.chmodErr
	}
	return f.OSFileOps.Chmod(path, mode)
}

func (f *fakeFileOps) Rename(src, dst string) error {
	if f.renameErr != nil {
		return f.renameErr
	}
	return f.OSFileOps.Rename(src, dst)
}

func setupApplier(t *testing.T, dl Downloader, svc *fakeSVC, health HealthChecker, fops FileOps) (*Applier, string) {
	t.Helper()
	dir := t.TempDir()
	destPath := filepath.Join(dir, "tgproxy-cli")
	if err := os.WriteFile(destPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Applier{
		Downloader:        dl,
		ServiceController: svc,
		HealthChecker:     health,
		FileOps:           fops,
		TmpDir:            dir,
	}, destPath
}

func TestApply_Success(t *testing.T) {
	svc := &fakeSVC{}
	health := &fakeHealth{}
	a, dest := setupApplier(t, &fakeDownloader{}, svc, health, &fakeFileOps{})

	// Use ComponentTeleproxy so a service restart is expected.
	info := UpdateInfo{Component: ComponentTeleproxy, DownloadURL: "http://x", SHA256: "abc"}
	if err := a.Apply(info, dest); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new-binary" {
		t.Fatalf("binary not replaced: got %q", content)
	}
	if len(svc.restartCalls) != 1 {
		t.Fatalf("expected 1 restart call, got %d", len(svc.restartCalls))
	}
	if svc.restartCalls[0] != "teleproxy.service" {
		t.Fatalf("expected restart of teleproxy.service, got %q", svc.restartCalls[0])
	}
}

func TestApply_CLI_NoServiceRestart(t *testing.T) {
	svc := &fakeSVC{}
	health := &fakeHealth{}
	a, dest := setupApplier(t, &fakeDownloader{}, svc, health, &fakeFileOps{})

	// CLI has no associated service — no restart should occur.
	info := UpdateInfo{Component: ComponentCLI, DownloadURL: "http://x", SHA256: "abc"}
	if err := a.Apply(info, dest); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, _ := os.ReadFile(dest)
	if string(content) != "new-binary" {
		t.Fatalf("binary not replaced: got %q", content)
	}
	if len(svc.restartCalls) != 0 {
		t.Fatalf("CLI update must not restart any service, got calls: %v", svc.restartCalls)
	}
}

func TestApply_SHA256Failure_NoReplacement(t *testing.T) {
	svc := &fakeSVC{}
	health := &fakeHealth{}
	dl := &fakeDownloader{err: fmt.Errorf("sha256 mismatch")}
	a, dest := setupApplier(t, dl, svc, health, &fakeFileOps{})

	info := UpdateInfo{Component: ComponentCLI, DownloadURL: "http://x", SHA256: "bad"}
	if err := a.Apply(info, dest); err == nil {
		t.Fatal("expected error on download failure")
	}

	content, _ := os.ReadFile(dest)
	if string(content) != "old-binary" {
		t.Fatalf("binary was modified despite download failure: got %q", content)
	}
	if len(svc.restartCalls) != 0 {
		t.Fatalf("service should not be restarted on download failure")
	}
}

func TestApply_RestartFailure_RollsBack(t *testing.T) {
	svc := &fakeSVC{err: fmt.Errorf("systemd error")}
	health := &fakeHealth{}
	a, dest := setupApplier(t, &fakeDownloader{}, svc, health, &fakeFileOps{})

	// Use ComponentTeleproxy so a service restart is attempted and can fail.
	info := UpdateInfo{Component: ComponentTeleproxy, DownloadURL: "http://x", SHA256: "abc"}
	if err := a.Apply(info, dest); err == nil {
		t.Fatal("expected error on restart failure")
	}

	content, _ := os.ReadFile(dest)
	if string(content) != "old-binary" {
		t.Fatalf("binary should be rolled back after restart failure, got %q", content)
	}
}

func TestApply_HealthCheckFailure_RollsBack(t *testing.T) {
	svc := &fakeSVC{}
	// Health check always fails (even after rollback restart).
	health := &fakeHealth{failCount: 99}
	a, dest := setupApplier(t, &fakeDownloader{}, svc, health, &fakeFileOps{})

	info := UpdateInfo{Component: ComponentTeleproxy, DownloadURL: "http://x", SHA256: "abc"}
	err := a.Apply(info, dest)
	if err == nil {
		t.Fatal("expected error on health check failure")
	}

	content, _ := os.ReadFile(dest)
	if string(content) != "old-binary" {
		t.Fatalf("binary should be rolled back after health check failure, got %q", content)
	}

	// Two restart calls: one for update, one for rollback.
	if len(svc.restartCalls) != 2 {
		t.Fatalf("expected 2 restart calls (update + rollback), got %d", len(svc.restartCalls))
	}
}

func TestOSHealthChecker_TimeoutFails(t *testing.T) {
	now := time.Unix(0, 0)
	hc := OSHealthChecker{
		HealthCheckTimeout: 100 * time.Millisecond,
		PollInterval:       10 * time.Millisecond,
		isActive: func(service string) (string, error) {
			return "activating", nil
		},
		now:   func() time.Time { return now },
		sleep: func(d time.Duration) { now = now.Add(d) },
	}
	if err := hc.Check("svc"); err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestOSHealthChecker_BecomesActiveBeforeTimeout(t *testing.T) {
	now := time.Unix(0, 0)
	calls := 0
	hc := OSHealthChecker{
		HealthCheckTimeout: 5 * time.Second,
		PollInterval:       250 * time.Millisecond,
		isActive: func(service string) (string, error) {
			calls++
			// Simulate the service becoming active after ~1s of polling.
			if now.Sub(time.Unix(0, 0)) >= time.Second {
				return "active", nil
			}
			return "activating", nil
		},
		now:   func() time.Time { return now },
		sleep: func(d time.Duration) { now = now.Add(d) },
	}
	if err := hc.Check("svc"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if calls < 2 {
		t.Fatalf("expected multiple polls before active, got %d", calls)
	}
}

func TestOSHealthChecker_DefaultsApplied(t *testing.T) {
	hc := OSHealthChecker{
		isActive: func(string) (string, error) { return "active", nil },
	}
	if err := hc.Check("svc"); err != nil {
		t.Fatalf("expected success with defaults, got %v", err)
	}
}
