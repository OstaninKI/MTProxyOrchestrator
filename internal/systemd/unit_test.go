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
	BinaryPath: "/usr/local/bin/tgproxy-panel",
	ConfigPath: "/etc/tgproxy/config.toml",
	LogPath:    "/var/log/tgproxy/panel.log",
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
