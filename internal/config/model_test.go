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
	if !c.MSSClamp {
		t.Error("MSSClamp must default to true")
	}
	if c.RandomPadding {
		t.Error("RandomPadding must default to false (Fake-TLS)")
	}
	if !c.JA4Log {
		t.Error("JA4Log must default to true")
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
	want := config.InstallPaths{
		ConfigDir:       "/etc/tgproxy",
		LogDir:          "/var/log/tgproxy",
		BinDir:          "/usr/local/bin",
		SystemdDir:      "/etc/systemd/system",
		StubDir:         "/var/www/tgproxy-stub",
		CertDir:         "/etc/tgproxy/certs",
		NginxSnippetDir: "/etc/nginx/snippets",

		ConfigFile:    "/etc/tgproxy/config.toml",
		TeleproxyTOML: "/etc/tgproxy/teleproxy.toml",
		SingboxJSON:   "/etc/tgproxy/sing-box.json",
		UsersJSON:     "/etc/tgproxy/secrets/users.json",
		OutboundsJSON: "/etc/tgproxy/nodes/outbounds.json",
		PanelDB:       "/etc/tgproxy/panel.db",

		PanelLog:     "/var/log/tgproxy/panel.log",
		TeleproxyLog: "/var/log/tgproxy/teleproxy.log",
		SingboxLog:   "/var/log/tgproxy/sing-box.log",
		NginxLog:     "/var/log/tgproxy/nginx.log",

		TeleproxyBin: "/usr/local/bin/teleproxy",
		SingboxBin:   "/usr/local/bin/sing-box",
		CLIBin:       "/usr/local/bin/tgproxy-cli",
		PanelBin:     "/usr/local/bin/tgproxy-panel",

		TeleproxyService: "/etc/systemd/system/teleproxy.service",
		SingboxService:   "/etc/systemd/system/sing-box.service",
		PanelService:     "/etc/systemd/system/tgproxy-panel.service",
	}

	if got := config.DefaultPaths(); got != want {
		t.Errorf("DefaultPaths() mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}
