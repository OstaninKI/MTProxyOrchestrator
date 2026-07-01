package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/component"
)

// Downloader downloads and SHA256-verifies a binary to a destination path.
// SHA256 verification MUST happen inside Download before the file is placed
// at destPath. No existing file at destPath should be replaced on failure.
type Downloader interface {
	Download(url, sha256hex, destPath string) error
}

// TarGzDownloader extracts a single binary from a verified tar.gz archive.
// sing-box upstream publishes Linux builds as tar.gz assets, not raw binaries.
type TarGzDownloader interface {
	DownloadTarGzBinary(url, sha256hex, memberName, destPath string) error
}

// ServiceController restarts a named systemd service.
type ServiceController interface {
	Restart(serviceName string) error
}

// HealthChecker returns nil when the named service is healthy.
type HealthChecker interface {
	Check(serviceName string) error
}

// FileOps abstracts filesystem operations used during an update so they can
// be replaced in tests without touching the real filesystem.
type FileOps interface {
	// Copy copies src to dst preserving content. Creates or truncates dst.
	Copy(src, dst string) error
	// Rename atomically renames src to dst.
	Rename(src, dst string) error
	// Chmod changes the file mode bits of path.
	Chmod(path string, mode os.FileMode) error
	// Lchown changes the owning uid/gid of path without following symlinks.
	// A nil error from the no-op implementation is fine when ownership does not
	// matter (tests, non-Linux, or a process that already runs as the target uid).
	Lchown(path string, uid, gid int) error
	// StatOwner returns the uid/gid currently owning path.
	StatOwner(path string) (uid, gid int, err error)
	// Remove removes path (best effort; errors are silently ignored).
	Remove(path string)
}

// Applier applies a single UpdateInfo to the host.
type Applier struct {
	Downloader        Downloader
	ServiceController ServiceController
	HealthChecker     HealthChecker
	FileOps           FileOps
	TmpDir            string // defaults to os.TempDir()
}

// NewDefaultApplier returns an Applier wired to real system calls,
// including a real component.Downloader that performs SHA256-verified HTTP
// downloads.
func NewDefaultApplier() *Applier {
	return &Applier{
		Downloader:        component.Downloader{Client: http.DefaultClient},
		ServiceController: OSServiceController{},
		HealthChecker:     OSHealthChecker{HealthCheckTimeout: defaultHealthCheckTimeout},
		FileOps:           OSFileOps{},
	}
}

// componentService maps a Component to its systemd unit name.
// Returns an empty string for components with no long-running service.
func componentService(comp Component) string {
	switch comp {
	case ComponentPanel:
		return "tgproxy-panel.service"
	case ComponentTeleproxy:
		return "teleproxy.service"
	case ComponentSingbox:
		return "sing-box.service"
	default:
		// ComponentCLI and unknown components have no associated service.
		return ""
	}
}

// Apply downloads, SHA256-verifies, backs up, replaces, and health-checks
// a single component binary at destPath.
//
// Safety invariant: no existing file is overwritten until SHA256 verification
// succeeds inside Downloader.Download.
//
// On restart or health-check failure the original binary is restored from the
// backup and the service is restarted before the error is returned, ensuring
// the system is never left in a broken state.
func (a *Applier) Apply(info UpdateInfo, destPath string) error {
	if info.SHA256 == "" {
		return fmt.Errorf("update %s: SHA256 must not be empty", info.Component)
	}

	// In production destPath lives under /usr/local/bin (root-owned). A non-root
	// invoker would fail the rename below with an opaque EPERM; fail fast with a
	// clear message instead. TGPROXY_UPDATE_ALLOW_NONROOT=1 lets tests and
	// non-system install layouts proceed (the root check then reduces to a
	// best-effort chown that is a no-op when the caller already owns the file).
	if os.Geteuid() != 0 && os.Getenv("TGPROXY_UPDATE_ALLOW_NONROOT") != "1" {
		return fmt.Errorf("update %s: must run as root (euid=%d); set TGPROXY_UPDATE_ALLOW_NONROOT=1 only for local test installs",
			info.Component, os.Geteuid())
	}

	tmpDir := a.TmpDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}

	// Step 1: download to a temp file; SHA256 verified inside Download before
	// tmpPath is written. The existing binary at destPath is untouched here.
	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("tgproxy-update-%s.tmp", info.Component))
	defer a.FileOps.Remove(tmpPath)

	if err := a.downloadCandidate(info, tmpPath); err != nil {
		return fmt.Errorf("update %s: download/verify: %w", info.Component, err)
	}

	// Step 2: back up the current binary before replacing it, and capture its
	// ownership so the replaced binary inherits the same uid/gid rather than
	// the updater process's identity.
	backupPath := destPath + ".bak"
	hasBackup := false
	origUID, origGID := -1, -1
	if err := a.FileOps.Copy(destPath, backupPath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("update %s: backup: %w", info.Component, err)
		}
		// No existing binary — proceed without a backup.
	} else {
		hasBackup = true
		if uid, gid, err := a.FileOps.StatOwner(destPath); err == nil {
			origUID, origGID = uid, gid
		}
	}

	// Step 3: set permissions on the candidate binary before replacing.
	if err := a.FileOps.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("update %s: chmod temp: %w", info.Component, err)
	}

	// Step 4: atomically replace the binary and restore its ownership. Rename
	// resets ownership to the invoking user, so chown back to the original
	// owner (best-effort: a non-root caller that legitimately reaches here via
	// the sentinel may lack permission, and the file is still usable).
	if err := a.FileOps.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("update %s: replace binary: %w", info.Component, err)
	}
	if origUID >= 0 {
		if err := a.FileOps.Lchown(destPath, origUID, origGID); err != nil {
			return fmt.Errorf("update %s: restore ownership: %w", info.Component, err)
		}
	}

	svc := componentService(info.Component)

	// Step 5: restart the affected service (skip for CLI which has none).
	if svc != "" {
		if err := a.ServiceController.Restart(svc); err != nil {
			if hasBackup {
				_ = a.rollback(destPath, backupPath, svc)
			}
			return fmt.Errorf("update %s: restart %s: %w", info.Component, svc, err)
		}

		// Step 6: verify the service is healthy after the restart.
		if err := a.HealthChecker.Check(svc); err != nil {
			if hasBackup {
				rollbackErr := a.rollback(destPath, backupPath, svc)
				if rollbackErr != nil {
					return fmt.Errorf("update %s: unhealthy after update (%w); rollback failed: %v",
						info.Component, err, rollbackErr)
				}
				return fmt.Errorf("update %s: unhealthy after update, rolled back: %w",
					info.Component, err)
			}
			return fmt.Errorf("update %s: unhealthy after update (no backup to restore): %w",
				info.Component, err)
		}
	}

	// Clean up backup on success.
	if hasBackup {
		a.FileOps.Remove(backupPath)
	}
	return nil
}

func (a *Applier) downloadCandidate(info UpdateInfo, tmpPath string) error {
	if info.Component == ComponentSingbox && strings.HasSuffix(strings.ToLower(info.DownloadURL), ".tar.gz") {
		archiveDownloader, ok := a.Downloader.(TarGzDownloader)
		if !ok {
			return fmt.Errorf("tar.gz extraction is required for sing-box update")
		}
		return archiveDownloader.DownloadTarGzBinary(info.DownloadURL, info.SHA256, "sing-box", tmpPath)
	}
	return a.Downloader.Download(info.DownloadURL, info.SHA256, tmpPath)
}

// rollback restores the backup binary to destPath, sets 0755, and restarts
// the service. Returns the first error encountered.
func (a *Applier) rollback(destPath, backupPath, service string) error {
	if err := a.FileOps.Copy(backupPath, destPath); err != nil {
		return fmt.Errorf("rollback copy: %w", err)
	}
	if err := a.FileOps.Chmod(destPath, 0o755); err != nil {
		return fmt.Errorf("rollback chmod: %w", err)
	}
	if uid, gid, err := a.FileOps.StatOwner(backupPath); err == nil {
		// best-effort: keep ownership consistent with the backup
		_ = a.FileOps.Lchown(destPath, uid, gid)
	}
	if service != "" {
		if err := a.ServiceController.Restart(service); err != nil {
			return fmt.Errorf("rollback restart: %w", err)
		}
	}
	return nil
}

// --- Production implementations ---

const systemctlTimeout = 10 * time.Second

// OSServiceController uses systemctl to restart services.
type OSServiceController struct{}

func (OSServiceController) Restart(service string) error {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "restart", service)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart %s: %s: %w", service, out, err)
	}
	return nil
}

// defaultHealthCheckTimeout is the polling window for service health after a
// restart. Heavier services (panel with DB migrations / cache warm-up) can take
// noticeably longer than the original 5s budget, so the default is 15s.
const defaultHealthCheckTimeout = 15 * time.Second

// defaultHealthCheckPollInterval is how often is-active is polled within the
// timeout window.
const defaultHealthCheckPollInterval = 250 * time.Millisecond

// OSHealthChecker checks service health via systemctl is-active. It tolerates
// the brief `activating` window after a restart by polling until the configured
// timeout elapses and fails fast on `failed`.
//
// HealthCheckTimeout overrides the default polling window when non-zero.
// PollInterval overrides the default poll interval when non-zero.
// isActive is an optional override for tests; nil falls back to systemctlIsActive.
// now is an optional clock override for tests; nil falls back to time.Now.
// sleep is an optional sleep override for tests; nil falls back to time.Sleep.
type OSHealthChecker struct {
	HealthCheckTimeout time.Duration
	PollInterval       time.Duration

	isActive func(service string) (string, error)
	now      func() time.Time
	sleep    func(time.Duration)
}

func (h OSHealthChecker) Check(service string) error {
	timeout := h.HealthCheckTimeout
	if timeout <= 0 {
		timeout = defaultHealthCheckTimeout
	}
	pollInterval := h.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultHealthCheckPollInterval
	}
	isActive := h.isActive
	if isActive == nil {
		isActive = systemctlIsActive
	}
	nowFn := h.now
	if nowFn == nil {
		nowFn = time.Now
	}
	sleepFn := h.sleep
	if sleepFn == nil {
		sleepFn = time.Sleep
	}

	deadline := nowFn().Add(timeout)
	var lastState string
	for {
		state, err := isActive(service)
		lastState = state
		if err == nil && state == "active" {
			return nil
		}
		if state == "failed" {
			return fmt.Errorf("service %s is failed", service)
		}
		if !nowFn().Before(deadline) {
			break
		}
		sleepFn(pollInterval)
	}
	if lastState == "" {
		return fmt.Errorf("service %s is not active", service)
	}
	return fmt.Errorf("service %s is not active (state=%s)", service, lastState)
}

func systemctlIsActive(service string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", service).Output()
	state := strings.TrimSpace(string(out))
	if state == "" && err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			state = strings.TrimSpace(string(ee.Stderr))
		}
	}
	return state, err
}

// OSFileOps delegates to the real OS.
type OSFileOps struct{}

func (OSFileOps) Copy(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func (OSFileOps) Rename(src, dst string) error {
	return os.Rename(src, filepath.Clean(dst))
}

func (OSFileOps) Chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}

// Lchown changes ownership without following symlinks. syscall.Stat_t works on
// both Linux (production) and darwin (dev/test); both expose Uid/Gid as uint32.
func (OSFileOps) Lchown(path string, uid, gid int) error {
	return os.Lchown(path, uid, gid)
}

func (OSFileOps) StatOwner(path string) (int, int, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return -1, -1, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, -1, fmt.Errorf("stat: cannot read owner for %s", path)
	}
	return int(st.Uid), int(st.Gid), nil
}

func (OSFileOps) Remove(path string) {
	os.Remove(path) //nolint:errcheck
}
