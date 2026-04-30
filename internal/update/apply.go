package update

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Downloader downloads and SHA256-verifies a binary to a destination path.
// SHA256 verification MUST happen inside Download before the file is placed
// at destPath. No existing file at destPath should be replaced on failure.
type Downloader interface {
	Download(url, sha256hex, destPath string) error
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

// NewDefaultApplier returns an Applier wired to real system calls.
// The caller must replace Downloader with a real implementation before use;
// OSDownloader.Download returns an error directing the caller to do so.
func NewDefaultApplier() *Applier {
	return &Applier{
		Downloader:        OSDownloader{},
		ServiceController: OSServiceController{},
		HealthChecker:     OSHealthChecker{},
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

	tmpDir := a.TmpDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}

	// Step 1: download to a temp file; SHA256 verified inside Download before
	// tmpPath is written. The existing binary at destPath is untouched here.
	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("tgproxy-update-%s.tmp", info.Component))
	defer a.FileOps.Remove(tmpPath)

	if err := a.Downloader.Download(info.DownloadURL, info.SHA256, tmpPath); err != nil {
		return fmt.Errorf("update %s: download/verify: %w", info.Component, err)
	}

	// Step 2: back up the current binary before replacing it.
	backupPath := destPath + ".bak"
	hasBackup := false
	if err := a.FileOps.Copy(destPath, backupPath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("update %s: backup: %w", info.Component, err)
		}
		// No existing binary — proceed without a backup.
	} else {
		hasBackup = true
	}

	// Step 3: set permissions on the candidate binary before replacing.
	if err := a.FileOps.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("update %s: chmod temp: %w", info.Component, err)
	}

	// Step 4: atomically replace the binary.
	if err := a.FileOps.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("update %s: replace binary: %w", info.Component, err)
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

// rollback restores the backup binary to destPath, sets 0755, and restarts
// the service. Returns the first error encountered.
func (a *Applier) rollback(destPath, backupPath, service string) error {
	if err := a.FileOps.Copy(backupPath, destPath); err != nil {
		return fmt.Errorf("rollback copy: %w", err)
	}
	if err := a.FileOps.Chmod(destPath, 0o755); err != nil {
		return fmt.Errorf("rollback chmod: %w", err)
	}
	if service != "" {
		if err := a.ServiceController.Restart(service); err != nil {
			return fmt.Errorf("rollback restart: %w", err)
		}
	}
	return nil
}

// --- Production implementations ---

// OSDownloader is a stub that directs callers to inject a real downloader.
// In production, wire component.Downloader instead.
type OSDownloader struct{}

func (OSDownloader) Download(_, _, _ string) error {
	return fmt.Errorf("OSDownloader: inject a real Downloader (e.g. component.Downloader)")
}

// OSServiceController uses systemctl to restart services.
type OSServiceController struct{}

func (OSServiceController) Restart(service string) error {
	cmd := exec.Command("systemctl", "restart", service)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart %s: %s: %w", service, out, err)
	}
	return nil
}

// OSHealthChecker checks service health via systemctl is-active.
type OSHealthChecker struct{}

func (OSHealthChecker) Check(service string) error {
	if err := exec.Command("systemctl", "is-active", service).Run(); err != nil {
		return fmt.Errorf("service %s is not active: %w", service, err)
	}
	return nil
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

func (OSFileOps) Remove(path string) {
	os.Remove(path) //nolint:errcheck
}
