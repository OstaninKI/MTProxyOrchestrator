package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/acme"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/secrets"
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
func (stubExecutor) ReloadService(string) error                       { return nil }
func (stubExecutor) EnableNginxSite(string) error                     { return nil }

type stubPreflightRunner struct {
	result     install.CheckResult
	panelPort  int
	extraPorts []int
}

func (s *stubPreflightRunner) Run(panelPort int, extraPorts ...int) install.CheckResult {
	s.panelPort = panelPort
	s.extraPorts = append([]int(nil), extraPorts...)
	return s.result
}

func TestRunInstallChecksPanelBackendPort(t *testing.T) {
	oldUnattended := unattended
	oldChecker := defaultChecker
	oldResolve := resolveLocalBinaries
	oldBuild := buildSinglePlan
	oldExec := newExecutor
	oldPostInstall := runPostInstallCheck
	t.Cleanup(func() {
		unattended = oldUnattended
		defaultChecker = oldChecker
		resolveLocalBinaries = oldResolve
		buildSinglePlan = oldBuild
		newExecutor = oldExec
		runPostInstallCheck = oldPostInstall
	})

	unattended = true
	runner := &stubPreflightRunner{}
	defaultChecker = func() preflightRunner { return runner }
	resolveLocalBinaries = func() (install.LocalBinaries, error) {
		return install.LocalBinaries{CLI: "/tmp/tgproxy-cli", Panel: "/tmp/tgproxy-panel"}, nil
	}
	buildSinglePlan = func(config.Config, config.InstallPaths, int, install.LocalBinaries) (install.Plan, error) {
		return install.Plan{}, nil
	}
	newExecutor = func() install.Executor { return stubExecutor{} }
	runPostInstallCheck = func() error { return nil }

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := runInstall(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if runner.panelPort != 8443 {
		t.Fatalf("panel port = %d, want 8443", runner.panelPort)
	}
	if len(runner.extraPorts) != 1 || runner.extraPorts[0] != install.PanelBackendPort {
		t.Fatalf("extra ports = %v, want [%d]", runner.extraPorts, install.PanelBackendPort)
	}
}

func TestRunInstallPassesPanelTLSFlagsToPlan(t *testing.T) {
	oldUnattended := unattended
	oldPanelDomain := panelDomain
	oldPanelCert := panelCert
	oldPanelKey := panelKey
	oldChecker := defaultChecker
	oldResolve := resolveLocalBinaries
	oldBuild := buildSinglePlan
	oldExec := newExecutor
	oldPostInstall := runPostInstallCheck
	t.Cleanup(func() {
		unattended = oldUnattended
		panelDomain = oldPanelDomain
		panelCert = oldPanelCert
		panelKey = oldPanelKey
		defaultChecker = oldChecker
		resolveLocalBinaries = oldResolve
		buildSinglePlan = oldBuild
		newExecutor = oldExec
		runPostInstallCheck = oldPostInstall
	})

	unattended = true
	panelDomain = "proxy.example.com"
	panelCert = "/etc/tgproxy/certs/proxy.example.com/cert.pem"
	panelKey = "/etc/tgproxy/certs/proxy.example.com/key.pem"
	defaultChecker = func() preflightRunner { return &stubPreflightRunner{} }
	resolveLocalBinaries = func() (install.LocalBinaries, error) {
		return install.LocalBinaries{CLI: "/tmp/tgproxy-cli", Panel: "/tmp/tgproxy-panel"}, nil
	}

	var got config.Config
	buildSinglePlan = func(cfg config.Config, _ config.InstallPaths, _ int, _ install.LocalBinaries) (install.Plan, error) {
		got = cfg
		return install.Plan{
			Creds: install.GeneratedCreds{
				PanelPath:     "/p-test/",
				AdminLogin:    "admin",
				AdminPassword: "password",
				FirstUser:     secrets.UserSecret{Label: "user1"},
			},
		}, nil
	}
	newExecutor = func() install.Executor { return stubExecutor{} }
	runPostInstallCheck = func() error { return nil }

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := runInstall(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if got.PanelDomain != panelDomain || got.PanelCertPath != panelCert || got.PanelKeyPath != panelKey {
		t.Fatalf("panel TLS config not passed to plan: %+v", got)
	}
}

func TestRunInstallRejectsPartialPanelTLSFlags(t *testing.T) {
	oldUnattended := unattended
	oldPanelDomain := panelDomain
	oldPanelCert := panelCert
	oldPanelKey := panelKey
	oldChecker := defaultChecker
	oldResolve := resolveLocalBinaries
	t.Cleanup(func() {
		unattended = oldUnattended
		panelDomain = oldPanelDomain
		panelCert = oldPanelCert
		panelKey = oldPanelKey
		defaultChecker = oldChecker
		resolveLocalBinaries = oldResolve
	})

	unattended = true
	panelDomain = "proxy.example.com"
	panelCert = "/etc/tgproxy/certs/proxy.example.com/cert.pem"
	panelKey = ""
	defaultChecker = func() preflightRunner { return &stubPreflightRunner{} }
	resolveLocalBinaries = func() (install.LocalBinaries, error) {
		t.Fatal("resolveLocalBinaries must not run with partial panel TLS config")
		return install.LocalBinaries{}, nil
	}

	err := runInstall(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected error for partial panel TLS flags")
	}
	if !strings.Contains(err.Error(), "panel-domain, panel-cert, and panel-key") {
		t.Fatalf("unexpected error: %v", err)
	}
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
		return &stubPreflightRunner{
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

func TestRunInstallRejectsEmailWithoutDomain(t *testing.T) {
	oldUnattended := unattended
	oldPanelDomain := panelDomain
	oldPanelEmail := panelEmail
	t.Cleanup(func() {
		unattended = oldUnattended
		panelDomain = oldPanelDomain
		panelEmail = oldPanelEmail
	})

	unattended = true
	panelDomain = ""
	panelEmail = "admin@example.com"

	err := runInstall(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected error when --panel-email given without --panel-domain")
	}
	if !strings.Contains(err.Error(), "--panel-email requires --panel-domain") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInstallRejectsEmailAndManualCertTogether(t *testing.T) {
	oldUnattended := unattended
	oldPanelDomain := panelDomain
	oldPanelCert := panelCert
	oldPanelKey := panelKey
	oldPanelEmail := panelEmail
	t.Cleanup(func() {
		unattended = oldUnattended
		panelDomain = oldPanelDomain
		panelCert = oldPanelCert
		panelKey = oldPanelKey
		panelEmail = oldPanelEmail
	})

	unattended = true
	panelDomain = "proxy.example.com"
	panelCert = "/etc/tgproxy/certs/cert.pem"
	panelKey = "/etc/tgproxy/certs/key.pem"
	panelEmail = "admin@example.com"

	err := runInstall(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected error when --panel-email and --panel-cert/key both provided")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInstallCallsHealthCheckOnSuccess(t *testing.T) {
	oldUnattended := unattended
	oldChecker := defaultChecker
	oldResolve := resolveLocalBinaries
	oldBuild := buildSinglePlan
	oldExec := newExecutor
	oldPostInstall := runPostInstallCheck
	t.Cleanup(func() {
		unattended = oldUnattended
		defaultChecker = oldChecker
		resolveLocalBinaries = oldResolve
		buildSinglePlan = oldBuild
		newExecutor = oldExec
		runPostInstallCheck = oldPostInstall
	})

	unattended = true
	defaultChecker = func() preflightRunner { return &stubPreflightRunner{} }
	resolveLocalBinaries = func() (install.LocalBinaries, error) {
		return install.LocalBinaries{CLI: "/tmp/tgproxy-cli", Panel: "/tmp/tgproxy-panel"}, nil
	}
	buildSinglePlan = func(config.Config, config.InstallPaths, int, install.LocalBinaries) (install.Plan, error) {
		return install.Plan{}, nil
	}
	newExecutor = func() install.Executor { return stubExecutor{} }

	called := false
	runPostInstallCheck = func() error { called = true; return nil }

	var out strings.Builder
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	err := runInstall(cmd, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("runPostInstallCheck was not called")
	}
	if !strings.Contains(out.String(), "Services healthy") {
		t.Fatalf("expected 'Services healthy' in output, got: %s", out.String())
	}
}

func TestRunInstallStopsServicesOnHealthCheckFailure(t *testing.T) {
	oldUnattended := unattended
	oldChecker := defaultChecker
	oldResolve := resolveLocalBinaries
	oldBuild := buildSinglePlan
	oldExec := newExecutor
	oldPostInstall := runPostInstallCheck
	oldStop := stopServiceFn
	t.Cleanup(func() {
		unattended = oldUnattended
		defaultChecker = oldChecker
		resolveLocalBinaries = oldResolve
		buildSinglePlan = oldBuild
		newExecutor = oldExec
		runPostInstallCheck = oldPostInstall
		stopServiceFn = oldStop
	})

	unattended = true
	defaultChecker = func() preflightRunner { return &stubPreflightRunner{} }
	resolveLocalBinaries = func() (install.LocalBinaries, error) {
		return install.LocalBinaries{CLI: "/tmp/tgproxy-cli", Panel: "/tmp/tgproxy-panel"}, nil
	}
	buildSinglePlan = func(config.Config, config.InstallPaths, int, install.LocalBinaries) (install.Plan, error) {
		return install.Plan{}, nil
	}
	newExecutor = func() install.Executor { return stubExecutor{} }

	stopped := []string{}
	stopServiceFn = func(name string) { stopped = append(stopped, name) }
	runPostInstallCheck = func() error { return fmt.Errorf("not healthy") }

	var stderr strings.Builder
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(&stderr)
	err := runInstall(cmd, nil)
	if err == nil {
		t.Fatal("expected error from health check failure")
	}
	if !strings.Contains(err.Error(), "not healthy") {
		t.Fatalf("unexpected error: %v", err)
	}
	found := func(name string) bool {
		for _, s := range stopped {
			if s == name {
				return true
			}
		}
		return false
	}
	if !found("teleproxy.service") {
		t.Errorf("expected teleproxy.service to be stopped, got: %v", stopped)
	}
	if !found("tgproxy-panel.service") {
		t.Errorf("expected tgproxy-panel.service to be stopped, got: %v", stopped)
	}
}

func TestRunInstallCallsObtainACMEWhenEmailSet(t *testing.T) {
	oldUnattended := unattended
	oldPanelDomain := panelDomain
	oldPanelEmail := panelEmail
	oldChecker := defaultChecker
	oldResolve := resolveLocalBinaries
	oldBuild := buildSinglePlan
	oldExec := newExecutor
	oldObtain := obtainACMECert
	oldPostInstall := runPostInstallCheck
	t.Cleanup(func() {
		unattended = oldUnattended
		panelDomain = oldPanelDomain
		panelEmail = oldPanelEmail
		defaultChecker = oldChecker
		resolveLocalBinaries = oldResolve
		buildSinglePlan = oldBuild
		newExecutor = oldExec
		obtainACMECert = oldObtain
		runPostInstallCheck = oldPostInstall
	})

	unattended = true
	panelDomain = "proxy.example.com"
	panelEmail = "admin@example.com"
	defaultChecker = func() preflightRunner { return &stubPreflightRunner{} }
	resolveLocalBinaries = func() (install.LocalBinaries, error) {
		return install.LocalBinaries{CLI: "/tmp/tgproxy-cli", Panel: "/tmp/tgproxy-panel"}, nil
	}

	var gotDomain, gotEmail string
	obtainACMECert = func(_ context.Context, _ acme.Runner, domain, email string) (string, string, error) {
		gotDomain = domain
		gotEmail = email
		return "/etc/tgproxy/certs/proxy.example.com/cert.pem", "/etc/tgproxy/certs/proxy.example.com/key.pem", nil
	}

	var gotCfg config.Config
	buildSinglePlan = func(cfg config.Config, _ config.InstallPaths, _ int, _ install.LocalBinaries) (install.Plan, error) {
		gotCfg = cfg
		return install.Plan{
			Creds: install.GeneratedCreds{
				PanelPath:     "/p-test/",
				AdminLogin:    "admin",
				AdminPassword: "password",
				FirstUser:     secrets.UserSecret{Label: "user1"},
			},
		}, nil
	}
	newExecutor = func() install.Executor { return stubExecutor{} }
	runPostInstallCheck = func() error { return nil }

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := runInstall(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if gotDomain != "proxy.example.com" {
		t.Errorf("ObtainCert domain = %s, want proxy.example.com", gotDomain)
	}
	if gotEmail != "admin@example.com" {
		t.Errorf("ObtainCert email = %s, want admin@example.com", gotEmail)
	}
	if gotCfg.PanelCertPath == "" || gotCfg.PanelKeyPath == "" {
		t.Error("cert paths must be set in config after ACME obtain")
	}
	if gotCfg.ACMEEmail != "admin@example.com" {
		t.Errorf("ACMEEmail in config = %s, want admin@example.com", gotCfg.ACMEEmail)
	}
}
