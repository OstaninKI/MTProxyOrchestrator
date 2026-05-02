package teleproxy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
)

func TestDetectModeBridge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teleproxy.toml")
	if err := os.WriteFile(path, []byte("port = 443\nsocks5 = \"127.0.0.1:1080\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	mode, err := DetectMode(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode != config.ModeBridge {
		t.Fatalf("mode = %q, want %q", mode, config.ModeBridge)
	}
}

func TestDetectModeSingle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "teleproxy.toml")
	if err := os.WriteFile(path, []byte("port = 443\nmask_host = \"www.microsoft.com\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	mode, err := DetectMode(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode != config.ModeSingle {
		t.Fatalf("mode = %q, want %q", mode, config.ModeSingle)
	}
}
