package install_test

import (
	"encoding/hex"
	"encoding/json"
	"regexp"
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

func TestSinglePlanWritesRootConfigFile(t *testing.T) {
	cfg := config.Default()
	paths := config.DefaultPaths()

	plan, err := install.BuildSinglePlan(cfg, paths, 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range plan.Steps {
		if s.Kind != install.StepWriteFile || s.Target != paths.ConfigFile {
			continue
		}
		if s.Mode != 0o600 {
			t.Fatalf("config.toml mode = %04o, want 0600", s.Mode)
		}
		body := string(s.Content)
		for _, want := range []string{
			`mode = "single"`,
			`panel_path = "` + plan.Creds.PanelPath + `"`,
			`mask_host = "www.microsoft.com"`,
			`mtproto_port = 443`,
			`# [telegram_bot]`,
			`# token = ""`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("config.toml missing %q:\n%s", want, body)
			}
		}
		return
	}
	t.Fatalf("Single plan must write %s", paths.ConfigFile)
}

func TestSinglePlanPanelPathIsRandomNotAdminLogin(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan.Creds.PanelPath, plan.Creds.AdminLogin) {
		t.Fatalf("panel path %q must not contain admin login %q", plan.Creds.PanelPath, plan.Creds.AdminLogin)
	}
	if !regexp.MustCompile(`^/p-[a-z0-9]{8}/$`).MatchString(plan.Creds.PanelPath) {
		t.Fatalf("panel path %q must match /p-{8 lowercase alnum}/", plan.Creds.PanelPath)
	}
}

func TestBridgePlanAddsSingboxAndRoutesTeleproxyThroughSOCKS5(t *testing.T) {
	cfg := config.Default()
	cfg.BridgeStrategy = "selector"
	paths := config.DefaultPaths()
	const shareURL = "vless://11111111-1111-4111-8111-111111111111@bridge.example.com:443?security=reality&sni=www.cloudflare.com&pbk=abc123&sid=deadbeef&flow=xtls-rprx-vision#first"

	plan, err := install.BuildBridgePlan(cfg, paths, 8443, testLocalBinaries(), shareURL, "selector")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != config.ModeBridge {
		t.Fatalf("mode = %s, want bridge", plan.Mode)
	}

	var gotConfig, gotOutbounds, gotSingboxJSON, gotSingboxService, gotSingboxStart, gotTeleproxySOCKS bool
	for _, s := range plan.Steps {
		switch {
		case s.Kind == install.StepWriteFile && s.Target == paths.ConfigFile:
			gotConfig = strings.Contains(string(s.Content), `mode = "bridge"`) &&
				strings.Contains(string(s.Content), `bridge_strategy = "selector"`)
		case s.Kind == install.StepWriteFile && s.Target == paths.OutboundsJSON:
			gotOutbounds = strings.Contains(string(s.Content), `"strategy": "selector"`) &&
				strings.Contains(string(s.Content), `"host": "bridge.example.com"`)
		case s.Kind == install.StepWriteFile && s.Target == paths.SingboxJSON:
			gotSingboxJSON = strings.Contains(string(s.Content), `"type": "socks"`) &&
				strings.Contains(string(s.Content), `"type": "selector"`) &&
				strings.Contains(string(s.Content), `"type": "vless"`)
		case s.Kind == install.StepWriteFile && s.Target == paths.SingboxService:
			gotSingboxService = strings.Contains(string(s.Content), "sing-box run --config "+paths.SingboxJSON)
		case s.Kind == install.StepStartService && s.Target == "sing-box":
			gotSingboxStart = true
		case s.Kind == install.StepWriteFile && s.Target == paths.TeleproxyTOML:
			gotTeleproxySOCKS = strings.Contains(string(s.Content), `socks5 = "socks5://127.0.0.1:1080"`)
		}
	}
	for name, ok := range map[string]bool{
		"config.toml bridge mode": gotConfig,
		"outbounds.json":          gotOutbounds,
		"sing-box.json":           gotSingboxJSON,
		"sing-box.service":        gotSingboxService,
		"sing-box start":          gotSingboxStart,
		"teleproxy socks5":        gotTeleproxySOCKS,
	} {
		if !ok {
			t.Errorf("Bridge plan missing %s", name)
		}
	}
}

func TestBridgePlanRejectsNonVLESSFirstOutbound(t *testing.T) {
	_, err := install.BuildBridgePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries(), "trojan://secret@example.com:443?sni=example.com#x", "urltest")
	if err == nil {
		t.Fatal("expected non-VLESS first outbound to be rejected")
	}
	if !strings.Contains(err.Error(), "VLESS Reality") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBridgePlanOutboundsJSONIsValid(t *testing.T) {
	const shareURL = "vless://11111111-1111-4111-8111-111111111111@bridge.example.com:443?security=reality&sni=www.cloudflare.com&pbk=abc123&sid=deadbeef#first"
	plan, err := install.BuildBridgePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries(), shareURL, "urltest")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind == install.StepWriteFile && s.Target == config.DefaultPaths().OutboundsJSON {
			var doc map[string]any
			if err := json.Unmarshal(s.Content, &doc); err != nil {
				t.Fatalf("outbounds.json must be valid JSON: %v\n%s", err, s.Content)
			}
			return
		}
	}
	t.Fatal("Bridge plan must write outbounds.json")
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

func TestSinglePlanWritesTLSStubBackendWhenTLSConfigPresent(t *testing.T) {
	cfg := config.Default()
	cfg.MaskHost = "habr.com"
	cfg.PanelDomain = "proxy.example.com"
	cfg.PanelCertPath = "/etc/tgproxy/certs/proxy.example.com/cert.pem"
	cfg.PanelKeyPath = "/etc/tgproxy/certs/proxy.example.com/key.pem"

	plan, err := install.BuildSinglePlan(cfg, config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range plan.Steps {
		if s.Kind != install.StepWriteFile || s.Target != "/etc/nginx/sites-available/tgproxy-stub-tls" {
			continue
		}
		body := string(s.Content)
		for _, want := range []string{
			"listen 127.0.0.1:9443 ssl",
			"server_name proxy.example.com",
			"ssl_certificate     " + cfg.PanelCertPath,
			"root /var/www/tgproxy-stub",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("TLS stub config missing %q:\n%s", want, body)
			}
		}
		return
	}
	t.Fatal("Single plan must write a stub TLS backend when certificate config is present")
}

func TestSinglePlanUsesPanelDomainAsTeleproxyFallbackWhenTLSConfigPresent(t *testing.T) {
	cfg := config.Default()
	cfg.MaskHost = "habr.com"
	cfg.PanelDomain = "proxy.example.com"
	cfg.PanelCertPath = "/etc/tgproxy/certs/proxy.example.com/cert.pem"
	cfg.PanelKeyPath = "/etc/tgproxy/certs/proxy.example.com/key.pem"
	paths := config.DefaultPaths()

	plan, err := install.BuildSinglePlan(cfg, paths, 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	var teleproxyUsesLocalFallback, panelUnitUsesClientMask bool
	for _, s := range plan.Steps {
		if s.Kind == install.StepWriteFile && s.Target == paths.TeleproxyTOML {
			teleproxyUsesLocalFallback = strings.Contains(string(s.Content), `domain = [{ name = "proxy.example.com", backend = "127.0.0.1:9443" }]`)
		}
		if s.Kind == install.StepWriteFile && s.Target == paths.PanelService {
			panelUnitUsesClientMask = strings.Contains(string(s.Content), "--mask-host proxy.example.com")
		}
	}
	if !teleproxyUsesLocalFallback {
		t.Fatal("Teleproxy config must point probe fallback at the loopback TLS stub backend")
	}
	if !panelUnitUsesClientMask {
		t.Fatal("panel-generated user links must use the same public domain as Teleproxy expects")
	}
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

func TestSinglePlanEnsuresTeleproxyUserBeforeStartingService(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	userIdx := -1
	startIdx := -1
	for idx, s := range plan.Steps {
		if s.Kind == install.StepEnsureSystemUser && s.Target == "teleproxy" {
			userIdx = idx
		}
		if s.Kind == install.StepStartService && s.Target == "teleproxy" {
			startIdx = idx
		}
	}
	if userIdx < 0 {
		t.Fatal("Single plan must ensure the teleproxy system user exists")
	}
	if startIdx < 0 {
		t.Fatal("Single plan must start teleproxy")
	}
	if userIdx > startIdx {
		t.Fatalf("teleproxy user must be created before service start: user step %d, start step %d", userIdx, startIdx)
	}
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

func TestSinglePlanWritesACMESnippetWhenEmailConfigured(t *testing.T) {
	cfg := config.Default()
	cfg.PanelDomain = "proxy.example.com"
	cfg.ACMEEmail = "admin@example.com"
	paths := config.DefaultPaths()

	plan, err := install.BuildSinglePlan(cfg, paths, 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	wantSnippet := "/etc/nginx/snippets/acme-challenge.conf"
	var foundSnippet, foundSnippetDir, foundStubWithInclude bool
	for _, s := range plan.Steps {
		if s.Kind == install.StepWriteFile && s.Target == wantSnippet {
			foundSnippet = true
			body := string(s.Content)
			if !strings.Contains(body, "location /.well-known/acme-challenge/") {
				t.Fatalf("ACME snippet must contain challenge location block:\n%s", body)
			}
			if !strings.Contains(body, paths.CertDir) {
				t.Fatalf("ACME snippet must reference cert dir webroot:\n%s", body)
			}
		}
		if s.Kind == install.StepCreateDir && s.Target == "/etc/nginx/snippets" {
			foundSnippetDir = true
		}
		if s.Kind == install.StepWriteFile && s.Target == "/etc/nginx/sites-available/tgproxy-stub" {
			if strings.Contains(string(s.Content), wantSnippet) {
				foundStubWithInclude = true
			}
		}
	}
	if !foundSnippetDir {
		t.Error("Single plan must create /etc/nginx/snippets dir when ACME email is set")
	}
	if !foundSnippet {
		t.Errorf("Single plan must write ACME challenge snippet to %s", wantSnippet)
	}
	if !foundStubWithInclude {
		t.Error("Single plan must include ACME snippet path in stub nginx config")
	}
}

func TestSinglePlanACMESnippetAppearsBeforeStubConfig(t *testing.T) {
	cfg := config.Default()
	cfg.PanelDomain = "proxy.example.com"
	cfg.ACMEEmail = "admin@example.com"

	plan, err := install.BuildSinglePlan(cfg, config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	snippetIdx, stubIdx := -1, -1
	for i, s := range plan.Steps {
		if s.Kind == install.StepWriteFile && s.Target == "/etc/nginx/snippets/acme-challenge.conf" {
			snippetIdx = i
		}
		if s.Kind == install.StepWriteFile && s.Target == "/etc/nginx/sites-available/tgproxy-stub" {
			stubIdx = i
		}
	}
	if snippetIdx == -1 {
		t.Fatal("ACME snippet write step not found")
	}
	if stubIdx == -1 {
		t.Fatal("stub config write step not found")
	}
	if snippetIdx >= stubIdx {
		t.Errorf("ACME snippet step (idx %d) must come before stub config step (idx %d)", snippetIdx, stubIdx)
	}
}

func TestSinglePlanPanelUnitHasACMEFlagsWhenConfigured(t *testing.T) {
	cfg := config.Default()
	cfg.PanelDomain = "proxy.example.com"
	cfg.ACMEEmail = "admin@example.com"
	paths := config.DefaultPaths()

	plan, err := install.BuildSinglePlan(cfg, paths, 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range plan.Steps {
		if s.Kind != install.StepWriteFile || s.Target != paths.PanelService {
			continue
		}
		unit := string(s.Content)
		if !strings.Contains(unit, "--cert-dir "+paths.CertDir) {
			t.Fatalf("panel unit must pass --cert-dir when ACME is configured:\n%s", unit)
		}
		if !strings.Contains(unit, "--domain proxy.example.com") {
			t.Fatalf("panel unit must pass --domain when ACME is configured:\n%s", unit)
		}
		if !strings.Contains(unit, "--acme-email admin@example.com") {
			t.Fatalf("panel unit must pass --acme-email when ACME is configured:\n%s", unit)
		}
		return
	}
	t.Fatalf("panel service unit step not found at %s", paths.PanelService)
}

func TestSinglePlanDisablesDefaultNginxSite(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind == install.StepDisableNginxSite && s.Target == "default" {
			return
		}
	}
	t.Fatal("Single plan must disable the default nginx site to prevent port 80 conflict with tgproxy-stub")
}

func TestSinglePlanReloadsNginxAfterEnablingStubSite(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	enableIdx, reloadIdx := -1, -1
	for i, s := range plan.Steps {
		if s.Kind == install.StepEnableNginxSite && s.Target == "tgproxy-stub" {
			enableIdx = i
		}
		if enableIdx >= 0 && s.Kind == install.StepReloadService && s.Target == "nginx" && i > enableIdx {
			reloadIdx = i
			break
		}
	}
	if enableIdx < 0 {
		t.Fatal("Single plan must enable tgproxy-stub nginx site")
	}
	if reloadIdx < 0 {
		t.Fatalf("Single plan must reload nginx after enabling tgproxy-stub (enable at step %d, no reload found after)", enableIdx)
	}
}

func TestSinglePlanDisablesDefaultSiteBeforeEnablingStub(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	disableIdx, enableIdx := -1, -1
	for i, s := range plan.Steps {
		if s.Kind == install.StepDisableNginxSite && s.Target == "default" {
			disableIdx = i
		}
		if s.Kind == install.StepEnableNginxSite && s.Target == "tgproxy-stub" {
			enableIdx = i
		}
	}
	if disableIdx < 0 {
		t.Fatal("plan must include StepDisableNginxSite for default")
	}
	if enableIdx < 0 {
		t.Fatal("plan must include StepEnableNginxSite for tgproxy-stub")
	}
	if disableIdx > enableIdx {
		t.Fatalf("default nginx site must be disabled (step %d) before tgproxy-stub is enabled (step %d)", disableIdx, enableIdx)
	}
}

func TestSinglePlanPatchesNginxConfBeforeReload(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}

	patchIdx, reloadIdx := -1, -1
	for i, s := range plan.Steps {
		if s.Kind == install.StepPatchNginxConf && s.Target == "/etc/nginx/nginx.conf" {
			patchIdx = i
		}
		if s.Kind == install.StepReloadService && s.Target == "nginx" && reloadIdx < 0 {
			reloadIdx = i
		}
	}
	if patchIdx < 0 {
		t.Fatal("plan must include StepPatchNginxConf for /etc/nginx/nginx.conf")
	}
	if reloadIdx < 0 {
		t.Fatal("plan must include a nginx reload")
	}
	if patchIdx > reloadIdx {
		t.Fatalf("nginx.conf must be patched (step %d) before nginx is reloaded (step %d)", patchIdx, reloadIdx)
	}
}

func TestSinglePlanNoACMESnippetWithoutEmail(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443, testLocalBinaries())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind == install.StepWriteFile && strings.Contains(s.Target, "acme-challenge") {
			t.Fatalf("Single plan must not write ACME snippet when ACMEEmail is empty: %+v", s)
		}
	}
}
