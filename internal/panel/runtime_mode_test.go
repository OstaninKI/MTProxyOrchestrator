package panel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
)

func TestIsBridgeModeUsesTeleproxyConfig(t *testing.T) {
	dir := t.TempDir()
	teleproxyPath := filepath.Join(dir, "teleproxy.toml")
	if err := os.WriteFile(teleproxyPath, []byte("port = 443\nsocks5 = \"127.0.0.1:1080\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := &Server{
		BridgeCfg: &BridgeConfig{
			Paths: config.InstallPaths{
				TeleproxyTOML: teleproxyPath,
			},
		},
	}
	if !srv.isBridgeMode() {
		t.Fatal("expected bridge mode when teleproxy config contains socks5 upstream")
	}
}

func TestBridgePathsUsesDefaultsWhenBridgeConfigHasNoPaths(t *testing.T) {
	srv := &Server{
		BridgeCfg: &BridgeConfig{
			MTProtoPort: 443,
			MaskHost:    "www.microsoft.com",
			StatsPort:   9091,
		},
	}

	got := srv.bridgePaths()
	if got.TeleproxyTOML != config.DefaultPaths().TeleproxyTOML {
		t.Fatalf("TeleproxyTOML = %q, want default %q", got.TeleproxyTOML, config.DefaultPaths().TeleproxyTOML)
	}
	if got.OutboundsJSON != config.DefaultPaths().OutboundsJSON {
		t.Fatalf("OutboundsJSON = %q, want default %q", got.OutboundsJSON, config.DefaultPaths().OutboundsJSON)
	}
}
