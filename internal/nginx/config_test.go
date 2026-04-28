package nginx_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/nginx"
)

const updateGolden = false

var baseCfg = nginx.StubConfig{
	ListenPort: 80,
	ServerName: "_",
	StubRoot:   "/var/www/tgproxy-stub",
}

func TestStubRenderServerTokensOff(t *testing.T) {
	out := baseCfg.Render()
	if !bytes.Contains(out, []byte("server_tokens off")) {
		t.Error("output must contain server_tokens off")
	}
}

func TestStubRenderLoopbackOnly(t *testing.T) {
	out := baseCfg.Render()
	if !bytes.Contains(out, []byte("127.0.0.1")) {
		t.Error("output must contain 127.0.0.1 in listen directive")
	}
}

func TestRenderGolden(t *testing.T) {
	got := baseCfg.Render()
	path := filepath.Join("testdata", "stub.conf")
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
		t.Errorf("render mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}
