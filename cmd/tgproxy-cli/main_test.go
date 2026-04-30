package main

import (
	"os"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
	"github.com/spf13/cobra"
)

type stubExecutor struct{}

func (stubExecutor) CreateDir(string, os.FileMode) error              { return nil }
func (stubExecutor) WriteFile(string, []byte, os.FileMode) error      { return nil }
func (stubExecutor) Download(string, string, string) error            { return nil }
func (stubExecutor) InstallFile(string, string, os.FileMode) error    { return nil }
func (stubExecutor) InitPanelDB(string, install.PanelBootstrap) error { return nil }
func (stubExecutor) AptInstall(...string) error                       { return nil }
func (stubExecutor) EnableService(string) error                       { return nil }
func (stubExecutor) StartService(string) error                        { return nil }

type stubPreflightRunner struct {
	result install.CheckResult
}

func (s stubPreflightRunner) Run(int) install.CheckResult {
	return s.result
}

func TestRunInstallFailsPreflight(t *testing.T) {
	oldUnattended := unattended
	oldChecker := defaultChecker
	oldResolve := resolveLocalBinaries
	oldBuild := buildSinglePlan
	oldExec := newExecutor
	t.Cleanup(func() {
		unattended = oldUnattended
		defaultChecker = oldChecker
		resolveLocalBinaries = oldResolve
		buildSinglePlan = oldBuild
		newExecutor = oldExec
	})

	unattended = true
	defaultChecker = func() preflightRunner {
		return stubPreflightRunner{
			result: install.CheckResult{
				Errors: []install.CheckError{{
					Check:       "root",
					Description: "not running as root",
					Remediation: "Run as root",
				}},
			},
		}
	}
	resolveLocalBinaries = func() (install.LocalBinaries, error) {
		t.Fatal("resolveLocalBinaries must not run after failed preflight")
		return install.LocalBinaries{}, nil
	}
	buildSinglePlan = func(config.Config, config.InstallPaths, int, install.LocalBinaries) (install.Plan, error) {
		t.Fatal("buildSinglePlan must not run after failed preflight")
		return install.Plan{}, nil
	}
	newExecutor = func() install.Executor {
		t.Fatal("newExecutor must not run after failed preflight")
		return stubExecutor{}
	}

	err := runInstall(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected preflight error")
	}
	if !strings.Contains(err.Error(), "not running as root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveLocalBinariesRequiresSiblingPanelBinary(t *testing.T) {
	dir := t.TempDir()
	cliPath := dir + "/tgproxy-cli"
	panelPath := dir + "/tgproxy-panel"
	if err := os.WriteFile(cliPath, []byte("cli"), 0o755); err != nil {
		t.Fatal(err)
	}

	bins, err := siblingLocalBinaries(cliPath)
	if err == nil || !strings.Contains(err.Error(), panelPath) {
		t.Fatalf("expected missing panel binary error, got bins=%+v err=%v", bins, err)
	}

	if err := os.WriteFile(panelPath, []byte("panel"), 0o755); err != nil {
		t.Fatal(err)
	}
	bins, err = siblingLocalBinaries(cliPath)
	if err != nil {
		t.Fatal(err)
	}
	if bins.CLI != cliPath || bins.Panel != panelPath {
		t.Fatalf("resolved bins mismatch: %+v", bins)
	}
}
