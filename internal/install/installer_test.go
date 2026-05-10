package install_test

import (
	"errors"
	"io"
	"log"
	"os"
	"slices"
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
	users     []string
	enables   []string
	starts    []string
	reloads   []string
	sites     []string
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

func (f *fakeExecutor) EnsureSystemUser(name string) error {
	f.users = append(f.users, name)
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

func (f *fakeExecutor) ReloadService(name string) error {
	f.reloads = append(f.reloads, name)
	return nil
}

func (f *fakeExecutor) EnableNginxSite(name string) error {
	f.sites = append(f.sites, name)
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

func TestInstallerEnsuresTeleproxySystemUser(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	fe := &fakeExecutor{}
	inst := install.Installer{Executor: fe, Plan: plan}
	if err := inst.Run(); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(fe.users, "teleproxy") {
		t.Fatalf("installer must ensure teleproxy system user, got %v", fe.users)
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

type rollbackEvent struct {
	Op     string
	Target string
}

type rollbackExecutor struct {
	failOn    int
	calls     int
	rollbacks []rollbackEvent
}

func (r *rollbackExecutor) record() error {
	r.calls++
	if r.calls == r.failOn {
		return errors.New("forced failure")
	}
	return nil
}

func (r *rollbackExecutor) CreateDir(_ string, _ os.FileMode) error { return r.record() }

func (r *rollbackExecutor) WriteFile(_ string, _ []byte, _ os.FileMode) error { return r.record() }

func (r *rollbackExecutor) Download(_, _, _ string) error { return r.record() }

func (r *rollbackExecutor) InstallFile(_, _ string, _ os.FileMode) error { return r.record() }

func (r *rollbackExecutor) InitPanelDB(_ string, _ install.PanelBootstrap) error {
	return r.record()
}

func (r *rollbackExecutor) AptInstall(_ ...string) error { return r.record() }

func (r *rollbackExecutor) EnsureSystemUser(_ string) error { return r.record() }

func (r *rollbackExecutor) EnableService(_ string) error { return r.record() }

func (r *rollbackExecutor) StartService(_ string) error { return r.record() }

func (r *rollbackExecutor) ReloadService(_ string) error { return r.record() }

func (r *rollbackExecutor) EnableNginxSite(_ string) error { return r.record() }

func (r *rollbackExecutor) RemoveFile(path string) error {
	r.rollbacks = append(r.rollbacks, rollbackEvent{Op: "RemoveFile", Target: path})
	return nil
}

func (r *rollbackExecutor) RemoveDir(path string) error {
	r.rollbacks = append(r.rollbacks, rollbackEvent{Op: "RemoveDir", Target: path})
	return nil
}

func (r *rollbackExecutor) RemoveService(name string) error {
	r.rollbacks = append(r.rollbacks, rollbackEvent{Op: "RemoveService", Target: name})
	return nil
}

func (r *rollbackExecutor) RemoveNginxSite(name string) error {
	r.rollbacks = append(r.rollbacks, rollbackEvent{Op: "RemoveNginxSite", Target: name})
	return nil
}

func TestInstallRollsBackOnMidPlanFailure(t *testing.T) {
	plan := install.Plan{Steps: []install.Step{
		{Kind: install.StepCreateDir, Target: "/etc/tgproxy"},
		{Kind: install.StepWriteFile, Target: "/etc/tgproxy/config.toml"},
		{Kind: install.StepEnableService, Target: "tgproxy-panel"},
		{Kind: install.StepStartService, Target: "tgproxy-panel"},
	}}

	re := &rollbackExecutor{failOn: 4}
	inst := install.Installer{Executor: re, Plan: plan, Logger: log.New(io.Discard, "", 0)}
	if err := inst.Run(); err == nil {
		t.Fatal("expected error, got nil")
	}

	want := []rollbackEvent{
		{Op: "RemoveService", Target: "tgproxy-panel"},
		{Op: "RemoveFile", Target: "/etc/tgproxy/config.toml"},
		{Op: "RemoveDir", Target: "/etc/tgproxy"},
	}
	if len(re.rollbacks) != len(want) {
		t.Fatalf("rollback events: got %d (%+v), want %d", len(re.rollbacks), re.rollbacks, len(want))
	}
	for i, ev := range want {
		if re.rollbacks[i] != ev {
			t.Errorf("rollback[%d]: got %+v, want %+v", i, re.rollbacks[i], ev)
		}
	}
}

func TestInstallCoalescesAptInstall(t *testing.T) {
	plan := install.Plan{Steps: []install.Step{
		{Kind: install.StepAptInstall, Target: "nginx"},
		{Kind: install.StepAptInstall, Target: "curl"},
		{Kind: install.StepAptInstall, Target: "ca-certificates"},
		{Kind: install.StepCreateDir, Target: "/etc/tgproxy"},
	}}

	fe := &fakeExecutor{}
	inst := install.Installer{Executor: fe, Plan: plan}
	if err := inst.Run(); err != nil {
		t.Fatal(err)
	}

	if got := len(fe.apts); got != 3 {
		t.Fatalf("expected 3 packages installed in one call, got %d (%v)", got, fe.apts)
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
