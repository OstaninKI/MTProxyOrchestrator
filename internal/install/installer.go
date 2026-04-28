package install

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/component"
)

// Executor applies one Step to the host.
type Executor interface {
	CreateDir(path string, mode os.FileMode) error
	WriteFile(path string, content []byte, mode os.FileMode) error
	Download(url, sha256hex, destPath string) error
	AptInstall(packages ...string) error
	EnableService(name string) error
	StartService(name string) error
}

// Installer runs a Plan using a given Executor.
type Installer struct {
	Executor Executor
	Plan     Plan
}

// Run applies Plan.Steps in order. Returns the first error encountered.
func (i Installer) Run() error {
	for _, step := range i.Plan.Steps {
		var err error
		switch step.Kind {
		case StepCreateDir:
			err = i.Executor.CreateDir(step.Target, step.Mode)
		case StepWriteFile:
			err = i.Executor.WriteFile(step.Target, step.Content, step.Mode)
		case StepDownloadBinary:
			err = i.Executor.Download(step.URL, step.SHA256, step.Target)
		case StepAptInstall:
			err = i.Executor.AptInstall(step.Target)
		case StepEnableService:
			err = i.Executor.EnableService(step.Target)
		case StepStartService:
			err = i.Executor.StartService(step.Target)
		default:
			return fmt.Errorf("unknown step kind: %s", step.Kind)
		}
		if err != nil {
			return err
		}
	}
	return nil
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
	return dl.Download(url, sha256hex, destPath)
}

func (e *SystemExecutor) AptInstall(packages ...string) error {
	args := append([]string{"apt-get", "install", "-y"}, packages...)
	cmd := exec.Command("sudo", args...)
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
