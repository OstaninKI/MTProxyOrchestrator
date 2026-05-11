package install

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/component"
)

// Executor applies one Step to the host.
type Executor interface {
	CreateDir(path string, mode os.FileMode) error
	WriteFile(path string, content []byte, mode os.FileMode) error
	Download(url, sha256hex, destPath string) error
	InstallFile(sourcePath, destPath string, mode os.FileMode) error
	InitPanelDB(path string, bootstrap PanelBootstrap) error
	AptInstall(packages ...string) error
	EnsureSystemUser(name string) error
	EnableService(name string) error
	StartService(name string) error
	ReloadService(name string) error
	EnableNginxSite(name string) error
	DisableNginxSite(name string) error
}

// Rollbacker is an optional Executor extension that knows how to undo
// previously applied steps. SystemExecutor implements it; tests may opt in.
type Rollbacker interface {
	RemoveFile(path string) error
	RemoveDir(path string) error
	RemoveService(name string) error
	RemoveNginxSite(name string) error
}

// Installer runs a Plan using a given Executor.
type Installer struct {
	Executor Executor
	Plan     Plan
	Logger   *log.Logger
}

func (i Installer) logger() *log.Logger {
	if i.Logger != nil {
		return i.Logger
	}
	return log.Default()
}

// Run applies Plan.Steps in order. On failure, previously-applied steps are
// rolled back in reverse order on a best-effort basis.
func (i Installer) Run() error {
	steps := coalesceAptInstalls(i.Plan.Steps)
	journal := make([]Step, 0, len(steps))
	for _, step := range steps {
		if err := i.applyStep(step); err != nil {
			i.rollbackJournal(journal)
			return err
		}
		journal = append(journal, step)
	}
	return nil
}

func (i Installer) applyStep(step Step) error {
	switch step.Kind {
	case StepCreateDir:
		return i.Executor.CreateDir(step.Target, step.Mode)
	case StepWriteFile:
		return i.Executor.WriteFile(step.Target, step.Content, step.Mode)
	case StepDownloadBinary:
		return i.Executor.Download(step.URL, step.SHA256, step.Target)
	case StepInstallFile:
		return i.Executor.InstallFile(step.Source, step.Target, step.Mode)
	case StepInitPanelDB:
		if step.Bootstrap == nil {
			return fmt.Errorf("init-panel-db step missing bootstrap data")
		}
		return i.Executor.InitPanelDB(step.Target, *step.Bootstrap)
	case StepAptInstall:
		pkgs := step.AptPackages
		if len(pkgs) == 0 && step.Target != "" {
			pkgs = []string{step.Target}
		}
		return i.Executor.AptInstall(pkgs...)
	case StepEnsureSystemUser:
		return i.Executor.EnsureSystemUser(step.Target)
	case StepEnableService:
		return i.Executor.EnableService(step.Target)
	case StepStartService:
		return i.Executor.StartService(step.Target)
	case StepReloadService:
		return i.Executor.ReloadService(step.Target)
	case StepEnableNginxSite:
		return i.Executor.EnableNginxSite(step.Target)
	case StepDisableNginxSite:
		return i.Executor.DisableNginxSite(step.Target)
	default:
		return fmt.Errorf("unknown step kind: %s", step.Kind)
	}
}

func coalesceAptInstalls(in []Step) []Step {
	out := make([]Step, 0, len(in))
	for idx := 0; idx < len(in); idx++ {
		s := in[idx]
		if s.Kind != StepAptInstall {
			out = append(out, s)
			continue
		}
		pkgs := []string{}
		if len(s.AptPackages) > 0 {
			pkgs = append(pkgs, s.AptPackages...)
		} else if s.Target != "" {
			pkgs = append(pkgs, s.Target)
		}
		j := idx + 1
		for j < len(in) && in[j].Kind == StepAptInstall {
			n := in[j]
			if len(n.AptPackages) > 0 {
				pkgs = append(pkgs, n.AptPackages...)
			} else if n.Target != "" {
				pkgs = append(pkgs, n.Target)
			}
			j++
		}
		out = append(out, Step{Kind: StepAptInstall, AptPackages: pkgs})
		idx = j - 1
	}
	return out
}

func (i Installer) rollbackJournal(journal []Step) {
	rb, ok := i.Executor.(Rollbacker)
	for k := len(journal) - 1; k >= 0; k-- {
		step := journal[k]
		if !ok {
			i.logger().Printf("rollback: executor does not support rollback for %s %s", step.Kind, step.Target)
			continue
		}
		var err error
		switch step.Kind {
		case StepCreateDir:
			err = rb.RemoveDir(step.Target)
		case StepWriteFile, StepDownloadBinary, StepInstallFile, StepInitPanelDB:
			err = rb.RemoveFile(step.Target)
		case StepEnableService, StepStartService:
			err = rb.RemoveService(step.Target)
		case StepEnableNginxSite:
			err = rb.RemoveNginxSite(step.Target)
		case StepAptInstall, StepEnsureSystemUser, StepReloadService, StepDisableNginxSite:
			i.logger().Printf("rollback: skipping %s (no-op)", step.Kind)
			continue
		default:
			i.logger().Printf("rollback: unsupported step kind %s", step.Kind)
			continue
		}
		if err != nil {
			i.logger().Printf("rollback: %s %s: %v", step.Kind, step.Target, err)
		}
	}
}

// SystemExecutor is the real Executor that mutates the host.
type SystemExecutor struct{}

func NewSystemExecutor() *SystemExecutor { return &SystemExecutor{} }

func (e *SystemExecutor) CreateDir(path string, mode os.FileMode) error {
	return os.MkdirAll(path, mode)
}

func (e *SystemExecutor) WriteFile(path string, content []byte, mode os.FileMode) error {
	return os.WriteFile(path, content, mode)
}

func (e *SystemExecutor) Download(url, sha256hex, destPath string) error {
	dl := component.Downloader{Client: http.DefaultClient}
	if strings.HasSuffix(url, ".tar.gz") {
		return dl.DownloadTarGzBinary(url, sha256hex, filepath.Base(destPath), destPath)
	}
	return dl.Download(url, sha256hex, destPath)
}

func (e *SystemExecutor) InstallFile(sourcePath, destPath string, mode os.FileMode) error {
	if filepath.Clean(sourcePath) == filepath.Clean(destPath) {
		return os.Chmod(destPath, mode)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	srcAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	dstAbs, err := filepath.Abs(destPath)
	if err != nil {
		return err
	}
	if srcAbs == dstAbs {
		return os.Chmod(destPath, mode)
	}

	src, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return os.Chmod(destPath, mode)
}

func (e *SystemExecutor) InitPanelDB(path string, bootstrap PanelBootstrap) error {
	return BootstrapPanelDB(path, bootstrap)
}

func (e *SystemExecutor) AptInstall(packages ...string) error {
	args := append([]string{"apt-get", "install", "-y"}, packages...)
	cmd := exec.Command("sudo", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (e *SystemExecutor) EnsureSystemUser(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("system user name is required")
	}
	if err := exec.Command("id", "-u", name).Run(); err == nil {
		return nil
	}
	cmd := exec.Command("useradd", "--system", "--user-group", "--no-create-home", "--shell", "/usr/sbin/nologin", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (e *SystemExecutor) EnableService(name string) error {
	return exec.Command("systemctl", "enable", name).Run()
}

func (e *SystemExecutor) StartService(name string) error {
	return exec.Command("systemctl", "start", name).Run()
}

func (e *SystemExecutor) ReloadService(name string) error {
	return exec.Command("systemctl", "reload", name).Run()
}

func (e *SystemExecutor) EnableNginxSite(name string) error {
	src := filepath.Join("/etc/nginx/sites-available", filepath.Base(name))
	dst := filepath.Join("/etc/nginx/sites-enabled", filepath.Base(name))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if current, err := os.Readlink(dst); err == nil && current == src {
		return nil
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(src, dst)
}

func (e *SystemExecutor) RemoveFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (e *SystemExecutor) RemoveDir(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RemoveService disables and stops a unit, deletes its unit file from
// /etc/systemd/system, and reloads the daemon. Errors are best-effort.
func (e *SystemExecutor) RemoveService(name string) error {
	unit := name
	if !strings.Contains(unit, ".") {
		unit = unit + ".service"
	}
	_ = exec.Command("systemctl", "stop", unit).Run()
	_ = exec.Command("systemctl", "disable", unit).Run()
	unitPath := filepath.Join("/etc/systemd/system", unit)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return nil
}

func (e *SystemExecutor) RemoveNginxSite(name string) error {
	dst := filepath.Join("/etc/nginx/sites-enabled", filepath.Base(name))
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (e *SystemExecutor) DisableNginxSite(name string) error {
	dst := filepath.Join("/etc/nginx/sites-enabled", filepath.Base(name))
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
