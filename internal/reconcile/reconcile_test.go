package reconcile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/reconcile"
)

func setupReconcileEnv(t *testing.T, mode string) (string, config.InstallPaths) {
	t.Helper()
	tmp := t.TempDir()

	paths := config.InstallPaths{
		ConfigDir:       filepath.Join(tmp, "etc", "tgproxy"),
		LogDir:          filepath.Join(tmp, "var", "log", "tgproxy"),
		BinDir:          filepath.Join(tmp, "usr", "local", "bin"),
		SystemdDir:      filepath.Join(tmp, "etc", "systemd", "system"),
		StubDir:         filepath.Join(tmp, "var", "www", "tgproxy-stub"),
		CertDir:         filepath.Join(tmp, "etc", "tgproxy", "certs"),
		NginxSnippetDir: filepath.Join(tmp, "etc", "nginx", "snippets"),

		ConfigFile:    filepath.Join(tmp, "etc", "tgproxy", "config.toml"),
		TeleproxyTOML: filepath.Join(tmp, "etc", "tgproxy", "teleproxy.toml"),
		SingboxJSON:   filepath.Join(tmp, "etc", "tgproxy", "sing-box.json"),
		UsersJSON:     filepath.Join(tmp, "etc", "tgproxy", "secrets", "users.json"),
		OutboundsJSON: filepath.Join(tmp, "etc", "tgproxy", "nodes", "outbounds.json"),
		PanelDB:       filepath.Join(tmp, "etc", "tgproxy", "panel.db"),

		PanelLog:     filepath.Join(tmp, "var", "log", "tgproxy", "panel.log"),
		TeleproxyLog: filepath.Join(tmp, "var", "log", "tgproxy", "teleproxy.log"),
		SingboxLog:   filepath.Join(tmp, "var", "log", "tgproxy", "sing-box.log"),
		NginxLog:     filepath.Join(tmp, "var", "log", "tgproxy", "nginx.log"),

		TeleproxyBin: filepath.Join(tmp, "usr", "local", "bin", "teleproxy"),
		SingboxBin:   filepath.Join(tmp, "usr", "local", "bin", "sing-box"),
		CLIBin:       filepath.Join(tmp, "usr", "local", "bin", "tgproxy-cli"),
		PanelBin:     filepath.Join(tmp, "usr", "local", "bin", "tgproxy-panel"),

		TeleproxyService: filepath.Join(tmp, "etc", "systemd", "system", "teleproxy.service"),
		SingboxService:   filepath.Join(tmp, "etc", "systemd", "system", "sing-box.service"),
		PanelService:     filepath.Join(tmp, "etc", "systemd", "system", "tgproxy-panel.service"),
	}

	for _, dir := range []string{
		paths.ConfigDir,
		filepath.Join(paths.ConfigDir, "secrets"),
		filepath.Join(paths.ConfigDir, "nodes"),
		paths.SystemdDir,
		paths.LogDir,
		paths.StubDir,
		paths.BinDir,
		paths.CertDir,
		paths.NginxSnippetDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	writeConfigToml(t, paths.ConfigFile, mode)
	writeUsersJSON(t, paths.UsersJSON)
	createPanelDB(t, paths.PanelDB)

	return tmp, paths
}

func writeConfigToml(t *testing.T, path, mode string) {
	t.Helper()
	content := "mode = \"" + mode + "\"\n" +
		"mtproto_port = 443\n" +
		"mask_host = \"www.microsoft.com\"\n" +
		"bridge_strategy = \"urltest\"\n" +
		"log_level = \"info\"\n" +
		"tcp_keepalive_seconds = 60\n" +
		"panel_path = \"/p-test123/\"\n" +
		"panel_domain = \"\"\n" +
		"panel_cert_path = \"\"\n" +
		"panel_key_path = \"\"\n" +
		"acme_email = \"\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
}

func writeUsersJSON(t *testing.T, path string) {
	t.Helper()
	content := `{"users":[{"label":"testuser","secret":"aabbccddeeff00112233445566778899"}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write users.json: %v", err)
	}
}

func createPanelDB(t *testing.T, path string) *db.DB {
	t.Helper()
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func insertSetting(t *testing.T, d *db.DB, key, value string) {
	t.Helper()
	if err := d.SetSetting(key, value); err != nil {
		t.Fatalf("set setting %s=%s: %v", key, value, err)
	}
}

func callReconcileExpectNginxError(t *testing.T, opts reconcile.Options) {
	t.Helper()
	err := reconcile.Reconcile(opts)
	if err == nil {
		t.Fatal("expected error from Reconcile (nginx path not available in test), got nil")
	}
	if !strings.Contains(err.Error(), "write nginx stub") {
		t.Fatalf("expected error containing \"write nginx stub\", got: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestReconcileRendersTeleproxyUnit(t *testing.T) {
	_, paths := setupReconcileEnv(t, "single")

	opts := reconcile.Options{
		ConfigFile: paths.ConfigFile,
		PanelDB:    paths.PanelDB,
		Paths:      paths,
	}
	callReconcileExpectNginxError(t, opts)

	content := readFile(t, paths.TeleproxyService)
	if !strings.Contains(content, "ExecStart="+paths.TeleproxyBin) {
		t.Errorf("teleproxy unit missing ExecStart with binary path %q", paths.TeleproxyBin)
	}
	if !strings.Contains(content, "--config "+paths.TeleproxyTOML) {
		t.Errorf("teleproxy unit missing --config flag with path %q", paths.TeleproxyTOML)
	}
	if !strings.Contains(content, "StandardOutput=append:"+paths.TeleproxyLog) {
		t.Errorf("teleproxy unit missing StandardOutput with log path %q", paths.TeleproxyLog)
	}
}

func TestReconcileRendersPanelUnitWithOverrides(t *testing.T) {
	_, paths := setupReconcileEnv(t, "single")

	d := createPanelDB(t, paths.PanelDB)
	insertSetting(t, d, "mtproto_port", "8443")

	opts := reconcile.Options{
		ConfigFile: paths.ConfigFile,
		PanelDB:    paths.PanelDB,
		Paths:      paths,
	}
	callReconcileExpectNginxError(t, opts)

	content := readFile(t, paths.PanelService)
	if !strings.Contains(content, "--mtproto-port 8443") {
		t.Errorf("panel unit missing overridden --mtproto-port 8443, got:\n%s", content)
	}
	if !strings.Contains(content, "ExecStart="+paths.PanelBin+" serve") {
		t.Errorf("panel unit missing ExecStart with binary path %q", paths.PanelBin)
	}
	if !strings.Contains(content, "--listen 127.0.0.1:18080") {
		t.Errorf("panel unit missing --listen 127.0.0.1:18080")
	}
}

func TestReconcileRendersNginxStub(t *testing.T) {
	_, paths := setupReconcileEnv(t, "single")

	opts := reconcile.Options{
		ConfigFile: paths.ConfigFile,
		PanelDB:    paths.PanelDB,
		Paths:      paths,
	}
	err := reconcile.Reconcile(opts)
	if err == nil {
		t.Fatal("expected error from Reconcile (nginx hardcoded path not available), got nil")
	}
	if !strings.Contains(err.Error(), "write nginx stub") {
		t.Fatalf("expected error about nginx stub, got: %v", err)
	}

	if !fileExists(paths.TeleproxyService) {
		t.Error("teleproxy.service should have been written before nginx stub attempt")
	}
	if !fileExists(paths.PanelService) {
		t.Error("panel service should have been written before nginx stub attempt")
	}
	if !fileExists(paths.TeleproxyTOML) {
		t.Error("teleproxy.toml should have been written before nginx stub attempt")
	}
}

func TestReconcileSkipsSingboxInSingleMode(t *testing.T) {
	_, paths := setupReconcileEnv(t, "single")

	opts := reconcile.Options{
		ConfigFile: paths.ConfigFile,
		PanelDB:    paths.PanelDB,
		Paths:      paths,
	}
	callReconcileExpectNginxError(t, opts)

	if fileExists(paths.SingboxService) {
		data, _ := os.ReadFile(paths.SingboxService)
		t.Errorf("sing-box.service should NOT exist in single mode, found:\n%s", data)
	}
}

func TestReconcileWritesSingboxInBridgeMode(t *testing.T) {
	_, paths := setupReconcileEnv(t, "bridge")

	opts := reconcile.Options{
		ConfigFile: paths.ConfigFile,
		PanelDB:    paths.PanelDB,
		Paths:      paths,
	}
	callReconcileExpectNginxError(t, opts)

	content := readFile(t, paths.SingboxService)
	if !strings.Contains(content, "ExecStart="+paths.SingboxBin) {
		t.Errorf("sing-box unit missing ExecStart with binary path %q", paths.SingboxBin)
	}
	if !strings.Contains(content, "--config "+paths.SingboxJSON) {
		t.Errorf("sing-box unit missing --config flag with path %q", paths.SingboxJSON)
	}
	if !strings.Contains(content, "StandardOutput=append:"+paths.SingboxLog) {
		t.Errorf("sing-box unit missing StandardOutput with log path %q", paths.SingboxLog)
	}
}

func TestReconcileWritesTeleproxyToml(t *testing.T) {
	_, paths := setupReconcileEnv(t, "single")

	opts := reconcile.Options{
		ConfigFile: paths.ConfigFile,
		PanelDB:    paths.PanelDB,
		Paths:      paths,
	}
	callReconcileExpectNginxError(t, opts)

	content := readFile(t, paths.TeleproxyTOML)
	if !strings.Contains(content, "port = 443") {
		t.Errorf("teleproxy.toml missing port = 443, got:\n%s", content)
	}
	if !strings.Contains(content, `domain = "www.microsoft.com"`) {
		t.Errorf("teleproxy.toml missing domain, got:\n%s", content)
	}
	if !strings.Contains(content, `key = "aabbccddeeff00112233445566778899"`) {
		t.Errorf("teleproxy.toml missing user secret, got:\n%s", content)
	}
	if !strings.Contains(content, `label = "testuser"`) {
		t.Errorf("teleproxy.toml missing user label, got:\n%s", content)
	}
	if strings.Contains(content, "socks5") {
		t.Errorf("teleproxy.toml must not contain socks5 in single mode, got:\n%s", content)
	}
}

func TestReconcileBridgeTomlContainsSOCKS5(t *testing.T) {
	_, paths := setupReconcileEnv(t, "bridge")

	opts := reconcile.Options{
		ConfigFile: paths.ConfigFile,
		PanelDB:    paths.PanelDB,
		Paths:      paths,
	}
	callReconcileExpectNginxError(t, opts)

	content := readFile(t, paths.TeleproxyTOML)
	if !strings.Contains(content, `socks5 = "127.0.0.1:1080"`) {
		t.Errorf("teleproxy.toml missing socks5 address in bridge mode, got:\n%s", content)
	}
}

func TestReconcileDBSettingsOverrideConfig(t *testing.T) {
	_, paths := setupReconcileEnv(t, "single")

	d := createPanelDB(t, paths.PanelDB)
	insertSetting(t, d, "mtproto_port", "2083")

	var count int
	err := d.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = 'mtproto_port' AND value = '2083'`).Scan(&count)
	if err != nil {
		t.Fatalf("verify setting: %v", err)
	}
	if count != 1 {
		t.Fatalf("settings row not found, count=%d", count)
	}

	opts := reconcile.Options{
		ConfigFile: paths.ConfigFile,
		PanelDB:    paths.PanelDB,
		Paths:      paths,
	}
	callReconcileExpectNginxError(t, opts)

	content := readFile(t, paths.TeleproxyTOML)
	if !strings.Contains(content, "port = 2083") {
		t.Errorf("teleproxy.toml should use DB-overridden port 2083, got:\n%s", content)
	}
}
