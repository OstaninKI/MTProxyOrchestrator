package install_test

import (
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
)

func testLocalBinaries() install.LocalBinaries {
	return install.LocalBinaries{
		CLI:   "/tmp/tgproxy-cli",
		Panel: "/tmp/tgproxy-panel",
	}
}

func TestSinglePlanNoSingBox(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if strings.Contains(s.Target, "sing-box") || strings.Contains(string(s.URL), "sing-box") {
			t.Errorf("Single plan must not reference sing-box, got step: %+v", s)
		}
	}
}

func TestSinglePlanWritesPanelProxyWhenTLSConfigPresent(t *testing.T) {
	cfg := config.Default()
	cfg.PanelDomain = "proxy.example.com"
	cfg.PanelCertPath = "/etc/tgproxy/certs/proxy.example.com/cert.pem"
	cfg.PanelKeyPath = "/etc/tgproxy/certs/proxy.example.com/key.pem"
	paths := config.DefaultPaths()

	plan, err := install.BuildSinglePlan(cfg, paths, 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	wantTarget := "/etc/nginx/sites-available/tgproxy-panel"
	for _, s := range plan.Steps {
		if s.Kind != install.StepWriteFile || s.Target != wantTarget {
			continue
		}
		body := string(s.Content)
		if !strings.Contains(body, "server_name proxy.example.com") {
			t.Fatalf("panel proxy must use configured domain:\n%s", body)
		}
		if !strings.Contains(body, "ssl_certificate     "+cfg.PanelCertPath) {
			t.Fatalf("panel proxy must use configured certificate path:\n%s", body)
		}
		if !strings.Contains(body, "listen 0.0.0.0:8443 ssl") {
			t.Fatalf("panel proxy must listen on the public panel TLS port:\n%s", body)
		}
		if strings.Contains(body, "listen 0.0.0.0:443 ssl") {
			t.Fatalf("panel proxy must not bind Teleproxy's public MTProto port:\n%s", body)
		}
		if !strings.Contains(body, "proxy_pass http://127.0.0.1:18080") {
			t.Fatalf("panel proxy must route to loopback panel backend:\n%s", body)
		}
		return
	}
	t.Fatalf("Single plan must write nginx panel proxy config to %s when TLS config is present", wantTarget)
}

func TestSinglePlanEnablesAndReloadsNginxForPanelProxy(t *testing.T) {
	cfg := config.Default()
	cfg.PanelDomain = "proxy.example.com"
	cfg.PanelCertPath = "/etc/tgproxy/certs/proxy.example.com/cert.pem"
	cfg.PanelKeyPath = "/etc/tgproxy/certs/proxy.example.com/key.pem"

	plan, err := install.BuildSinglePlan(cfg, config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	var enabledPanel, reloadedNginx bool
	for _, s := range plan.Steps {
		if s.Kind == install.StepEnableNginxSite && s.Target == "tgproxy-panel" {
			enabledPanel = true
		}
		if s.Kind == install.StepReloadService && s.Target == "nginx" {
			reloadedNginx = true
		}
	}
	if !enabledPanel {
		t.Fatal("Single plan must enable the tgproxy-panel nginx site")
	}
	if !reloadedNginx {
		t.Fatal("Single plan must reload nginx after writing panel proxy config")
	}
}

func TestSinglePlanDoesNotWritePanelProxyWithoutTLSConfig(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind == install.StepWriteFile && strings.Contains(s.Target, "tgproxy-panel") && strings.Contains(string(s.Content), "ssl_certificate") {
			t.Fatalf("Single plan must not write public panel proxy without domain/cert config: %+v", s)
		}
	}
}

func TestSinglePlanHasTeleproxy(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind == install.StepDownloadBinary && strings.Contains(s.Target, "teleproxy") {
			return // found
		}
	}
	t.Error("Single plan must include a download-binary step for teleproxy")
}

func TestSinglePlanTeleproxyDownloadHasSHA256(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind != install.StepDownloadBinary || !strings.Contains(s.Target, "teleproxy") {
			continue
		}
		if len(s.SHA256) != 64 {
			t.Fatalf("teleproxy download SHA256 length: got %d, want 64", len(s.SHA256))
		}
		if _, err := hex.DecodeString(s.SHA256); err != nil {
			t.Fatalf("teleproxy download SHA256 must be hex: %v", err)
		}
		return
	}
	t.Fatal("Single plan must include a download-binary step for teleproxy")
}

func TestSinglePlanHasApt(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	var aptTargets []string
	for _, s := range plan.Steps {
		if s.Kind == install.StepAptInstall {
			aptTargets = append(aptTargets, s.Target)
		}
	}
	if !slices.Contains(aptTargets, "nginx") {
		t.Error("Single plan must include apt-install for nginx")
	}
}

func TestSinglePlanPanelUnitUsesGeneratedPath(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind != install.StepWriteFile || s.Target != config.DefaultPaths().PanelService {
			continue
		}
		unit := string(s.Content)
		if !strings.Contains(unit, "tgproxy-panel serve") {
			t.Fatalf("panel unit must run serve subcommand:\n%s", unit)
		}
		if !strings.Contains(unit, "--path "+plan.Creds.PanelPath) {
			t.Fatalf("panel unit must use generated panel path %q:\n%s", plan.Creds.PanelPath, unit)
		}
		if !strings.Contains(unit, "--db "+config.DefaultPaths().PanelDB) {
			t.Fatalf("panel unit must use panel DB path:\n%s", unit)
		}
		if !strings.Contains(unit, "--listen 127.0.0.1:18080") {
			t.Fatalf("panel unit must use loopback backend port:\n%s", unit)
		}
		if !strings.Contains(unit, "--mtproto-port 443") || !strings.Contains(unit, "--mask-host www.microsoft.com") || !strings.Contains(unit, "--stats-port 9091") {
			t.Fatalf("panel unit must pass teleproxy render settings:\n%s", unit)
		}
		return
	}
	t.Fatal("Single plan must write panel systemd unit")
}

func TestSinglePlanStartsPanelService(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	var enables, starts []string
	for _, s := range plan.Steps {
		switch s.Kind {
		case install.StepEnableService:
			enables = append(enables, s.Target)
		case install.StepStartService:
			starts = append(starts, s.Target)
		}
	}
	if !slices.Contains(enables, "tgproxy-panel") {
		t.Fatalf("Single plan must enable tgproxy-panel, got %v", enables)
	}
	if !slices.Contains(starts, "tgproxy-panel") {
		t.Fatalf("Single plan must start tgproxy-panel, got %v", starts)
	}
}

func TestSinglePlanInstallsLocalBinaries(t *testing.T) {
	paths := config.DefaultPaths()
	bins := testLocalBinaries()
	plan, err := install.BuildSinglePlan(config.Default(), paths, 8443, bins)
	if err != nil {
		t.Fatal(err)
	}

	var gotCLI, gotPanel bool
	for _, s := range plan.Steps {
		if s.Kind != install.StepInstallFile {
			continue
		}
		if s.Target == paths.CLIBin && s.Source == bins.CLI && s.Mode == 0o755 {
			gotCLI = true
		}
		if s.Target == paths.PanelBin && s.Source == bins.Panel && s.Mode == 0o755 {
			gotPanel = true
		}
	}

	if !gotCLI {
		t.Fatalf("Single plan must install local tgproxy-cli binary to %s", paths.CLIBin)
	}
	if !gotPanel {
		t.Fatalf("Single plan must install local tgproxy-panel binary to %s", paths.PanelBin)
	}
}

func TestSinglePlanConfigDirPermissions(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	// All StepCreateDir steps targeting /etc/tgproxy or its subdirectories must
	// never exceed mode 0700 (root-only access for config directories).
	for _, s := range plan.Steps {
		if s.Kind != install.StepCreateDir {
			continue
		}
		if !strings.HasPrefix(s.Target, "/etc/tgproxy") {
			continue
		}
		if s.Mode > 0o700 {
			t.Errorf("config dir %s has mode %04o, want <= 0700", s.Target, s.Mode)
		}
	}
}

func TestSinglePlanSecretsFilePermissions(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	// At least one StepWriteFile must target a path under secrets/ with mode 0600.
	for _, s := range plan.Steps {
		if s.Kind == install.StepWriteFile && strings.Contains(s.Target, "/secrets/") && s.Mode == 0o600 {
			return // found
		}
	}
	t.Error("Single plan must include a write-file step with mode 0600 for a file under secrets/")
}

func TestSinglePlanBinaryPermissions(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	// Every StepInstallFile step must use mode 0755.
	for _, s := range plan.Steps {
		if s.Kind != install.StepInstallFile {
			continue
		}
		if s.Mode != 0o755 {
			t.Errorf("binary install step for %s has mode %04o, want 0755", s.Target, s.Mode)
		}
	}
}

// TestSinglePlanConfigFilePermissions verifies that all StepWriteFile steps for
// config and secret files under /etc/tgproxy use mode 0600 (not 0644 or looser).
func TestSinglePlanConfigFilePermissions(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	paths := config.DefaultPaths()
	// These files hold secrets or configuration and must be mode 0600.
	sensitiveFiles := []string{
		paths.TeleproxyTOML,
		paths.UsersJSON,
	}
	for _, want := range sensitiveFiles {
		for _, s := range plan.Steps {
			if s.Kind == install.StepWriteFile && s.Target == want {
				if s.Mode != 0o600 {
					t.Errorf("config file %s has mode %04o, want 0600", s.Target, s.Mode)
				}
			}
		}
	}
}

// TestSinglePlanPanelDBPermissions verifies the panel DB init step uses mode 0600.
func TestSinglePlanPanelDBPermissions(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind != install.StepInitPanelDB {
			continue
		}
		if s.Mode != 0o600 {
			t.Errorf("panel DB init step has mode %04o, want 0600", s.Mode)
		}
		return
	}
	t.Fatal("Single plan must include panel DB bootstrap step")
}

// TestSinglePlanDownloadBinaryPermissions verifies that downloaded binaries use mode 0755.
func TestSinglePlanDownloadBinaryPermissions(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range plan.Steps {
		if s.Kind != install.StepDownloadBinary {
			continue
		}
		found = true
		if s.Mode != 0o755 {
			t.Errorf("downloaded binary %s has mode %04o, want 0755", s.Target, s.Mode)
		}
	}
	if !found {
		t.Error("Single plan must include at least one download-binary step")
	}
}

// TestSinglePlanNoOverlyPermissiveSecrets ensures no StepWriteFile for
// paths under /etc/tgproxy uses an overly permissive mode like 0644.
func TestSinglePlanNoOverlyPermissiveSecrets(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind != install.StepWriteFile {
			continue
		}
		if !strings.HasPrefix(s.Target, "/etc/tgproxy") {
			continue
		}
		// Config and secret files under /etc/tgproxy must not be world-readable.
		if s.Mode > 0o600 {
			t.Errorf("file %s under /etc/tgproxy has mode %04o, want <= 0600 (no world/group read for secrets)", s.Target, s.Mode)
		}
	}
}

func TestSinglePlanInitializesPanelDB(t *testing.T) {
	paths := config.DefaultPaths()
	plan, err := install.BuildSinglePlan(config.Default(), paths, 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range plan.Steps {
		if s.Kind != install.StepInitPanelDB {
			continue
		}
		if s.Target != paths.PanelDB {
			t.Fatalf("panel DB init target: got %s want %s", s.Target, paths.PanelDB)
		}
		if s.Bootstrap == nil {
			t.Fatal("panel DB init step must carry bootstrap data")
		}
		if s.Bootstrap.AdminLogin != plan.Creds.AdminLogin {
			t.Fatalf("admin login mismatch: got %s want %s", s.Bootstrap.AdminLogin, plan.Creds.AdminLogin)
		}
		if s.Bootstrap.UserLabel != plan.Creds.FirstUser.Label {
			t.Fatalf("first user label mismatch: got %s want %s", s.Bootstrap.UserLabel, plan.Creds.FirstUser.Label)
		}
		if s.Bootstrap.UserSecretHex != plan.Creds.FirstUser.Secret.Hex() {
			t.Fatal("first user secret in bootstrap does not match generated creds")
		}
		return
	}

	t.Fatal("Single plan must include panel DB bootstrap step")
}
