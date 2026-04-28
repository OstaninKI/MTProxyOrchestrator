package install_test

import (
	"os"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
)

type fakeExecutor struct {
	dirs      []string
	files     []string
	downloads []string
	apts      []string
	enables   []string
	starts    []string
}

func (f *fakeExecutor) CreateDir(path string, mode os.FileMode) error {
	f.dirs = append(f.dirs, path)
	return nil
}

func (f *fakeExecutor) WriteFile(path string, content []byte, mode os.FileMode) error {
	f.files = append(f.files, path)
	return nil
}

func (f *fakeExecutor) Download(url, sha256hex, destPath string) error {
	f.downloads = append(f.downloads, url+"|"+destPath)
	return nil
}

func (f *fakeExecutor) AptInstall(packages ...string) error {
	f.apts = append(f.apts, packages...)
	return nil
}

func (f *fakeExecutor) EnableService(name string) error {
	f.enables = append(f.enables, name)
	return nil
}

func (f *fakeExecutor) StartService(name string) error {
	f.starts = append(f.starts, name)
	return nil
}

func TestInstallUnattendedSingleNoBridge(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443)
	if err != nil {
		t.Fatal(err)
	}

	fe := &fakeExecutor{}
	inst := install.Installer{Executor: fe, Plan: plan}
	if err := inst.Run(); err != nil {
		t.Fatal(err)
	}

	for _, d := range fe.downloads {
		if strings.Contains(strings.ToLower(d), "sing-box") {
			t.Errorf("download references sing-box in Single mode: %s", d)
		}
	}
	for _, f := range fe.files {
		if strings.Contains(strings.ToLower(f), "sing-box") {
			t.Errorf("file target references sing-box in Single mode: %s", f)
		}
	}
}

func TestInstallRunsInOrder(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443)
	if err != nil {
		t.Fatal(err)
	}

	fe := &fakeExecutor{}
	inst := install.Installer{Executor: fe, Plan: plan}
	if err := inst.Run(); err != nil {
		t.Fatal(err)
	}

	if len(plan.Steps) == 0 {
		t.Fatal("plan has no steps")
	}
	if plan.Steps[0].Kind != install.StepAptInstall {
		t.Errorf("first step: got %s, want %s", plan.Steps[0].Kind, install.StepAptInstall)
	}

	hasTelepoxy := false
	for _, step := range plan.Steps {
		if step.Kind == install.StepDownloadBinary && strings.Contains(strings.ToLower(step.URL), "teleproxy") {
			hasTelepoxy = true
			break
		}
	}
	if !hasTelepoxy {
		t.Error("no StepDownloadBinary step found for teleproxy")
	}
}
