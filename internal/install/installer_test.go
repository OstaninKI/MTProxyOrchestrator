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
	installs  []string
	apts      []string
	enables   []string
	starts    []string
	seeds     []install.PanelBootstrap
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

func (f *fakeExecutor) InstallFile(sourcePath, destPath string, mode os.FileMode) error {
	f.installs = append(f.installs, sourcePath+"|"+destPath)
	return nil
}

func (f *fakeExecutor) InitPanelDB(path string, bootstrap install.PanelBootstrap) error {
	f.seeds = append(f.seeds, bootstrap)
	f.files = append(f.files, path)
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
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
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
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
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

func TestInstallSeedsPanelDB(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	fe := &fakeExecutor{}
	inst := install.Installer{Executor: fe, Plan: plan}
	if err := inst.Run(); err != nil {
		t.Fatal(err)
	}

	if len(fe.seeds) != 1 {
		t.Fatalf("expected exactly one panel DB seed call, got %d", len(fe.seeds))
	}
	if fe.seeds[0].AdminLogin != plan.Creds.AdminLogin {
		t.Fatalf("seed admin login mismatch: got %s want %s", fe.seeds[0].AdminLogin, plan.Creds.AdminLogin)
	}
	if fe.seeds[0].UserSecretHex != plan.Creds.FirstUser.Secret.Hex() {
		t.Fatal("seed first user secret does not match generated creds")
	}
}

func TestInstallCopiesLocalBinaries(t *testing.T) {
	paths := config.DefaultPaths()
	plan, err := install.BuildSinglePlan(config.Default(), paths, 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	fe := &fakeExecutor{}
	inst := install.Installer{Executor: fe, Plan: plan}
	if err := inst.Run(); err != nil {
		t.Fatal(err)
	}

	if len(fe.installs) != 2 {
		t.Fatalf("expected two local binary install calls, got %d", len(fe.installs))
	}
	if !strings.Contains(fe.installs[0], paths.CLIBin) && !strings.Contains(fe.installs[1], paths.CLIBin) {
		t.Fatalf("expected tgproxy-cli install target %s in %v", paths.CLIBin, fe.installs)
	}
	if !strings.Contains(fe.installs[0], paths.PanelBin) && !strings.Contains(fe.installs[1], paths.PanelBin) {
		t.Fatalf("expected tgproxy-panel install target %s in %v", paths.PanelBin, fe.installs)
	}
}

func TestSystemExecutorInstallFileNoopForSamePath(t *testing.T) {
	path := t.TempDir() + "/tgproxy-cli"
	want := []byte("binary")
	if err := os.WriteFile(path, want, 0o755); err != nil {
		t.Fatal(err)
	}

	exec := install.NewSystemExecutor()
	if err := exec.InstallFile(path, path, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("same-path install changed file contents: got %q want %q", got, want)
	}
}
