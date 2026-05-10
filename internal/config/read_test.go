package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
)

func TestReadConfigParsesAllFields(t *testing.T) {
	toml := []byte(`
mode = "bridge"
mtproto_port = 8443
mask_host = "cdn.example.com"
bridge_strategy = "random"
log_level = "debug"
tcp_keepalive_seconds = 120
panel_path = "/p-abc123/"
panel_domain = "panel.example.com"
panel_cert_path = "/etc/tgproxy/certs/panel.crt"
panel_key_path = "/etc/tgproxy/certs/panel.key"
acme_email = "admin@example.com"
`)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, toml, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.ReadConfig(path)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}

	if cfg.Mode != config.ModeBridge {
		t.Errorf("Mode = %q, want %q", cfg.Mode, config.ModeBridge)
	}
	if cfg.MTProtoPort != 8443 {
		t.Errorf("MTProtoPort = %d, want 8443", cfg.MTProtoPort)
	}
	if cfg.MaskHost != "cdn.example.com" {
		t.Errorf("MaskHost = %q, want cdn.example.com", cfg.MaskHost)
	}
	if cfg.BridgeStrategy != "random" {
		t.Errorf("BridgeStrategy = %q, want random", cfg.BridgeStrategy)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if cfg.TCPKeepalive != 120*time.Second {
		t.Errorf("TCPKeepalive = %v, want 120s", cfg.TCPKeepalive)
	}
	if cfg.PanelPath != "/p-abc123/" {
		t.Errorf("PanelPath = %q, want /p-abc123/", cfg.PanelPath)
	}
	if cfg.PanelDomain != "panel.example.com" {
		t.Errorf("PanelDomain = %q, want panel.example.com", cfg.PanelDomain)
	}
	if cfg.PanelCertPath != "/etc/tgproxy/certs/panel.crt" {
		t.Errorf("PanelCertPath = %q, want /etc/tgproxy/certs/panel.crt", cfg.PanelCertPath)
	}
	if cfg.PanelKeyPath != "/etc/tgproxy/certs/panel.key" {
		t.Errorf("PanelKeyPath = %q, want /etc/tgproxy/certs/panel.key", cfg.PanelKeyPath)
	}
	if cfg.ACMEEmail != "admin@example.com" {
		t.Errorf("ACMEEmail = %q, want admin@example.com", cfg.ACMEEmail)
	}
}

func TestReadConfigFillsDefaultsForMissingFields(t *testing.T) {
	toml := []byte(`mode = "single"`)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, toml, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.ReadConfig(path)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}

	if cfg.Mode != config.ModeSingle {
		t.Errorf("Mode = %q, want %q", cfg.Mode, config.ModeSingle)
	}
	if cfg.MTProtoPort != 443 {
		t.Errorf("MTProtoPort = %d, want 443", cfg.MTProtoPort)
	}
	if cfg.MaskHost != "www.microsoft.com" {
		t.Errorf("MaskHost = %q, want www.microsoft.com", cfg.MaskHost)
	}
	if cfg.BridgeStrategy != "urltest" {
		t.Errorf("BridgeStrategy = %q, want urltest", cfg.BridgeStrategy)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.TCPKeepalive != 60*time.Second {
		t.Errorf("TCPKeepalive = %v, want 60s", cfg.TCPKeepalive)
	}
}

func TestReadConfigReturnsErrorForMissingFile(t *testing.T) {
	_, err := config.ReadConfig("/nonexistent/path/config.toml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestReadConfigReturnsErrorForInvalidTOML(t *testing.T) {
	garbage := []byte(`{{{not valid toml!!`)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, garbage, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := config.ReadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid TOML, got nil")
	}
}

func TestReadConfigHandlesZeroTCPKeepalive(t *testing.T) {
	toml := []byte(`
mode = "single"
tcp_keepalive_seconds = 0
`)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, toml, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.ReadConfig(path)
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}

	if cfg.TCPKeepalive != 60*time.Second {
		t.Errorf("TCPKeepalive = %v, want 60s (default)", cfg.TCPKeepalive)
	}
}
