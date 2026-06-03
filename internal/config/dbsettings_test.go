package config_test

import (
	"path/filepath"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

func TestReadRuntimeSettingsFromEmptyDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	d.Close()

	rs, err := config.ReadRuntimeSettings(dbPath)
	if err != nil {
		t.Fatalf("ReadRuntimeSettings: %v", err)
	}
	if rs.MaskHost != "" {
		t.Errorf("MaskHost = %q, want empty", rs.MaskHost)
	}
	if rs.MTProtoPort != 0 {
		t.Errorf("MTProtoPort = %d, want 0", rs.MTProtoPort)
	}
	if rs.PanelPath != "" {
		t.Errorf("PanelPath = %q, want empty", rs.PanelPath)
	}
	if rs.LogLevel != "" {
		t.Errorf("LogLevel = %q, want empty", rs.LogLevel)
	}
}

func TestReadRuntimeSettingsReadsAllKeys(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	d.SetSetting("mask_host", "www.google.com")
	d.SetSetting("tls_backend", "127.0.0.1:9443")
	d.SetSetting("wildcard_mask", "*.example.com")
	d.SetSetting("mss_clamp", "false")
	d.SetSetting("random_padding", "false")
	d.SetSetting("ja4_log", "true")
	d.SetSetting("mtproto_port", "8443")
	d.SetSetting("panel_path", "/p-a8f3k2x9/")
	d.SetSetting("log_level", "debug")
	d.Close()

	rs, err := config.ReadRuntimeSettings(dbPath)
	if err != nil {
		t.Fatalf("ReadRuntimeSettings: %v", err)
	}
	if rs.MaskHost != "www.google.com" {
		t.Errorf("MaskHost = %q, want %q", rs.MaskHost, "www.google.com")
	}
	if rs.TLSBackend != "127.0.0.1:9443" {
		t.Errorf("TLSBackend = %q, want %q", rs.TLSBackend, "127.0.0.1:9443")
	}
	if rs.WildcardMask != "*.example.com" {
		t.Errorf("WildcardMask = %q, want %q", rs.WildcardMask, "*.example.com")
	}
	if rs.MSSClamp == nil || *rs.MSSClamp {
		t.Errorf("MSSClamp = %v, want false", rs.MSSClamp)
	}
	if rs.RandomPadding == nil || *rs.RandomPadding {
		t.Errorf("RandomPadding = %v, want false", rs.RandomPadding)
	}
	if rs.JA4Log == nil || !*rs.JA4Log {
		t.Errorf("JA4Log = %v, want true", rs.JA4Log)
	}
	if rs.MTProtoPort != 8443 {
		t.Errorf("MTProtoPort = %d, want 8443", rs.MTProtoPort)
	}
	if rs.PanelPath != "/p-a8f3k2x9/" {
		t.Errorf("PanelPath = %q, want %q", rs.PanelPath, "/p-a8f3k2x9/")
	}
	if rs.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", rs.LogLevel, "debug")
	}
}

func TestMergeIntoOverridesNonZero(t *testing.T) {
	cfg := config.Default()

	rs := config.RuntimeSettings{
		MaskHost:      "www.google.com",
		TLSBackend:    "127.0.0.1:9443",
		WildcardMask:  "*.example.com",
		MSSClamp:      boolPtr(false),
		RandomPadding: boolPtr(false),
		JA4Log:        boolPtr(false),
		MTProtoPort:   8443,
		PanelPath:     "/p-a8f3k2x9/",
		LogLevel:      "warn",
	}

	merged := rs.MergeInto(cfg)

	if merged.MaskHost != "www.google.com" {
		t.Errorf("MaskHost = %q, want %q", merged.MaskHost, "www.google.com")
	}
	if merged.TLSBackend != "127.0.0.1:9443" {
		t.Errorf("TLSBackend = %q, want %q", merged.TLSBackend, "127.0.0.1:9443")
	}
	if merged.WildcardMask != "*.example.com" {
		t.Errorf("WildcardMask = %q, want %q", merged.WildcardMask, "*.example.com")
	}
	if merged.MSSClamp {
		t.Error("MSSClamp = true, want false")
	}
	if merged.RandomPadding {
		t.Error("RandomPadding = true, want false")
	}
	if merged.JA4Log {
		t.Error("JA4Log = true, want false")
	}
	if merged.MTProtoPort != 8443 {
		t.Errorf("MTProtoPort = %d, want 8443", merged.MTProtoPort)
	}
	if merged.PanelPath != "/p-a8f3k2x9/" {
		t.Errorf("PanelPath = %q, want %q", merged.PanelPath, "/p-a8f3k2x9/")
	}
	if merged.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q", merged.LogLevel, "warn")
	}
}

func boolPtr(v bool) *bool { return &v }

func TestMergeIntoPreservesZero(t *testing.T) {
	cfg := config.Default()

	rs := config.RuntimeSettings{}

	merged := rs.MergeInto(cfg)

	if merged.MaskHost != cfg.MaskHost {
		t.Errorf("MaskHost = %q, want %q", merged.MaskHost, cfg.MaskHost)
	}
	if merged.MTProtoPort != cfg.MTProtoPort {
		t.Errorf("MTProtoPort = %d, want %d", merged.MTProtoPort, cfg.MTProtoPort)
	}
	if merged.LogLevel != cfg.LogLevel {
		t.Errorf("LogLevel = %q, want %q", merged.LogLevel, cfg.LogLevel)
	}
}

func TestReadRuntimeSettingsMissingDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nonexistent", "missing.db")

	rs, err := config.ReadRuntimeSettings(dbPath)
	if err != nil {
		t.Fatalf("ReadRuntimeSettings: %v", err)
	}
	if rs.MaskHost != "" {
		t.Errorf("MaskHost = %q, want empty", rs.MaskHost)
	}
	if rs.MTProtoPort != 0 {
		t.Errorf("MTProtoPort = %d, want 0", rs.MTProtoPort)
	}
	if rs.PanelPath != "" {
		t.Errorf("PanelPath = %q, want empty", rs.PanelPath)
	}
	if rs.LogLevel != "" {
		t.Errorf("LogLevel = %q, want empty", rs.LogLevel)
	}
}
