package systemd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/systemd"
)

const updateGolden = false

var baseTeleproxy = systemd.TeleproxyUnitConfig{
	BinaryPath: "/usr/local/bin/teleproxy",
	ConfigPath: "/etc/tgproxy/teleproxy.toml",
	LogPath:    "/var/log/tgproxy/teleproxy.log",
}

var basePanel = systemd.PanelUnitConfig{
	BinaryPath:    "/usr/local/bin/tgproxy-panel",
	ConfigPath:    "/etc/tgproxy/config.toml",
	DBPath:        "/etc/tgproxy/panel.db",
	PanelPath:     "/p-example/",
	ListenAddr:    "127.0.0.1:8443",
	MTProtoPort:   443,
	MaskHost:      "www.microsoft.com",
	MSSClamp:      true,
	RandomPadding: true,
	JA4Log:        true,
	StatsPort:     9091,
	LogPath:       "/var/log/tgproxy/panel.log",
	ConfigDir:     "/etc/tgproxy",
	LogDir:        "/var/log/tgproxy",
	BinDir:        "/usr/local/bin",
	SystemdDir:    "/etc/systemd/system",
}

func TestTeleproxyUnitHasNoNewPrivileges(t *testing.T) {
	got := baseTeleproxy.Render()
	if !bytes.Contains(got, []byte("NoNewPrivileges=yes")) {
		t.Error("teleproxy unit missing NoNewPrivileges=yes")
	}
}

func TestTeleproxyUnitHasNetBindCap(t *testing.T) {
	got := baseTeleproxy.Render()
	if !bytes.Contains(got, []byte("AmbientCapabilities=CAP_NET_BIND_SERVICE")) {
		t.Error("teleproxy unit missing AmbientCapabilities=CAP_NET_BIND_SERVICE")
	}
}

func TestTeleproxyUnitAllowsInternalPrivilegeDrop(t *testing.T) {
	got := baseTeleproxy.Render()
	for _, capName := range []string{"CAP_SETUID", "CAP_SETGID"} {
		if !bytes.Contains(got, []byte(capName)) {
			t.Errorf("teleproxy unit missing %s required for teleproxy's internal user switch:\n%s", capName, got)
		}
	}
}

func TestPanelUnitHasNoNewPrivileges(t *testing.T) {
	got := basePanel.Render()
	if !bytes.Contains(got, []byte("NoNewPrivileges=yes")) {
		t.Error("panel unit missing NoNewPrivileges=yes")
	}
}

func TestPanelUnitNoNetBindCap(t *testing.T) {
	got := basePanel.Render()
	if bytes.Contains(got, []byte("AmbientCapabilities=CAP_NET_BIND_SERVICE")) {
		t.Error("panel unit must not contain AmbientCapabilities=CAP_NET_BIND_SERVICE")
	}
}

func TestPanelUnitNoDynamicUser(t *testing.T) {
	got := basePanel.Render()
	if bytes.Contains(got, []byte("DynamicUser=yes")) {
		t.Error("panel unit must not contain DynamicUser=yes because panel writes root-owned state under /etc/tgproxy")
	}
}

func TestTeleproxyUnitNoDynamicUser(t *testing.T) {
	got := baseTeleproxy.Render()
	if bytes.Contains(got, []byte("DynamicUser=yes")) {
		t.Error("teleproxy unit must not contain DynamicUser=yes (needs CAP_NET_BIND_SERVICE)")
	}
}

func TestSingboxUnitNoDynamicUser(t *testing.T) {
	cfg := systemd.SingboxUnitConfig{
		BinaryPath: "/usr/local/bin/sing-box",
		ConfigPath: "/etc/tgproxy/sing-box.json",
		LogPath:    "/var/log/tgproxy/sing-box.log",
	}
	got := cfg.Render()
	if bytes.Contains(got, []byte("DynamicUser=yes")) {
		t.Error("sing-box unit must not contain DynamicUser=yes (needs port binding capabilities)")
	}
}

func TestPanelUnitDropsAllCapabilities(t *testing.T) {
	got := basePanel.Render()
	if !bytes.Contains(got, []byte("CapabilityBoundingSet=\n")) {
		t.Error("panel unit must have empty CapabilityBoundingSet to drop all capabilities")
	}
}

func TestPanelUnitHasRestrictedUmask(t *testing.T) {
	got := basePanel.Render()
	if !bytes.Contains(got, []byte("UMask=0077")) {
		t.Error("panel unit must set UMask=0077 so new state under /etc/tgproxy is not group/world accessible")
	}
}

func TestPanelUnitPassesServeFlags(t *testing.T) {
	got := basePanel.Render()
	for _, want := range []string{
		"tgproxy-panel serve",
		"--db /etc/tgproxy/panel.db",
		"--path /p-example/",
		"--listen 127.0.0.1:8443",
		"--mtproto-port 443",
		"--mask-host www.microsoft.com",
		"--mss-clamp=true",
		"--random-padding=true",
		"--ja4-log=true",
		"--stats-port 9091",
	} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("panel unit missing %q:\n%s", want, got)
		}
	}
}

func TestPanelUnitAllowsRequiredWritePaths(t *testing.T) {
	got := basePanel.Render()
	want := "ReadWritePaths=/etc/tgproxy /var/log/tgproxy /usr/local/bin /etc/systemd/system"
	if !bytes.Contains(got, []byte(want)) {
		t.Fatalf("panel unit must allow required write paths under ProtectSystem=strict:\n%s", got)
	}
}

var hardeningDirectives = []string{
	"PrivateDevices=yes",
	"ProtectKernelTunables=yes",
	"ProtectKernelModules=yes",
	"ProtectControlGroups=yes",
	"LockPersonality=yes",
	"RestrictRealtime=yes",
	"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
	"RestrictNamespaces=yes",
	"SystemCallFilter=@system-service",
}

func TestTeleproxyUnitHardening(t *testing.T) {
	got := baseTeleproxy.Render()
	for _, d := range hardeningDirectives {
		if !bytes.Contains(got, []byte(d)) {
			t.Errorf("teleproxy unit missing hardening directive: %s", d)
		}
	}
}

func TestPanelUnitHardening(t *testing.T) {
	got := basePanel.Render()
	for _, d := range hardeningDirectives {
		if !bytes.Contains(got, []byte(d)) {
			t.Errorf("panel unit missing hardening directive: %s", d)
		}
	}
}

func TestSingboxUnitOrderedBeforeTeleproxy(t *testing.T) {
	cfg := systemd.SingboxUnitConfig{
		BinaryPath: "/usr/local/bin/sing-box",
		ConfigPath: "/etc/tgproxy/sing-box.json",
		LogPath:    "/var/log/tgproxy/sing-box.log",
	}
	got := cfg.Render()
	if !bytes.Contains(got, []byte("Before=teleproxy.service")) {
		t.Error("sing-box unit missing Before=teleproxy.service in [Unit] section; teleproxy must start after sing-box on reboot")
	}
}

func TestSingboxUnitHardening(t *testing.T) {
	cfg := systemd.SingboxUnitConfig{
		BinaryPath: "/usr/local/bin/sing-box",
		ConfigPath: "/etc/tgproxy/sing-box.json",
		LogPath:    "/var/log/tgproxy/sing-box.log",
	}
	got := cfg.Render()
	for _, d := range hardeningDirectives {
		if !bytes.Contains(got, []byte(d)) {
			t.Errorf("sing-box unit missing hardening directive: %s", d)
		}
	}
}

func TestTeleproxyUnitGolden(t *testing.T) {
	got := baseTeleproxy.Render()
	checkGolden(t, "teleproxy.service", got)
}

func TestPanelUnitGolden(t *testing.T) {
	got := basePanel.Render()
	checkGolden(t, "panel.service", got)
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		t.Logf("updated golden file %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set updateGolden=true to generate)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("render mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}
