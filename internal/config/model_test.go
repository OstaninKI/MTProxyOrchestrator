package config_test

import (
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
)

func TestDefaultConfig(t *testing.T) {
	c := config.Default()

	if c.Mode != config.ModeSingle {
		t.Errorf("Mode = %q, want %q", c.Mode, config.ModeSingle)
	}
	if c.MTProtoPort != 443 {
		t.Errorf("MTProtoPort = %d, want 443", c.MTProtoPort)
	}
	if c.MaskHost != "www.microsoft.com" {
		t.Errorf("MaskHost = %q, want www.microsoft.com", c.MaskHost)
	}
	if c.BridgeStrategy != "urltest" {
		t.Errorf("BridgeStrategy = %q, want urltest", c.BridgeStrategy)
	}
	if c.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", c.LogLevel)
	}
	if c.TCPKeepalive != 60*time.Second {
		t.Errorf("TCPKeepalive = %v, want 60s", c.TCPKeepalive)
	}
}

func TestDefaultPaths(t *testing.T) {
	p := config.DefaultPaths()

	checks := map[string]string{
		"ConfigDir":     "/etc/tgproxy",
		"LogDir":        "/var/log/tgproxy",
		"BinDir":        "/usr/local/bin",
		"SystemdDir":    "/etc/systemd/system",
		"StubDir":       "/var/www/tgproxy-stub",
		"ConfigFile":    "/etc/tgproxy/config.toml",
		"TeleproxyTOML": "/etc/tgproxy/teleproxy.toml",
		"SingboxJSON":   "/etc/tgproxy/sing-box.json",
		"UsersJSON":     "/etc/tgproxy/secrets/users.json",
		"OutboundsJSON": "/etc/tgproxy/nodes/outbounds.json",
		"PanelDB":       "/etc/tgproxy/panel.db",
		"PanelLog":      "/var/log/tgproxy/panel.log",
		"TeleproxyLog":  "/var/log/tgproxy/teleproxy.log",
		"SingboxLog":    "/var/log/tgproxy/sing-box.log",
		"NginxLog":      "/var/log/tgproxy/nginx.log",
		"TeleproxyBin":  "/usr/local/bin/teleproxy",
		"SingboxBin":    "/usr/local/bin/sing-box",
		"CLIBin":        "/usr/local/bin/tgproxy-cli",
		"PanelBin":          "/usr/local/bin/tgproxy-panel",
		"TeleproxyService": "/etc/systemd/system/teleproxy.service",
		"SingboxService":   "/etc/systemd/system/sing-box.service",
		"PanelService":     "/etc/systemd/system/tgproxy-panel.service",
	}
	v := map[string]string{
		"ConfigDir":        p.ConfigDir,
		"LogDir":           p.LogDir,
		"BinDir":           p.BinDir,
		"SystemdDir":       p.SystemdDir,
		"StubDir":          p.StubDir,
		"ConfigFile":       p.ConfigFile,
		"TeleproxyTOML":    p.TeleproxyTOML,
		"SingboxJSON":      p.SingboxJSON,
		"UsersJSON":        p.UsersJSON,
		"OutboundsJSON":    p.OutboundsJSON,
		"PanelDB":          p.PanelDB,
		"PanelLog":         p.PanelLog,
		"TeleproxyLog":     p.TeleproxyLog,
		"SingboxLog":       p.SingboxLog,
		"NginxLog":         p.NginxLog,
		"TeleproxyBin":     p.TeleproxyBin,
		"SingboxBin":       p.SingboxBin,
		"CLIBin":           p.CLIBin,
		"PanelBin":         p.PanelBin,
		"TeleproxyService": p.TeleproxyService,
		"SingboxService":   p.SingboxService,
		"PanelService":     p.PanelService,
	}
	for name, want := range checks {
		if got := v[name]; got != want {
			t.Errorf("Paths.%s = %q, want %q", name, got, want)
		}
	}
}
