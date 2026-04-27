package teleproxy_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
)

const updateGolden = false

var (
	userAlice = teleproxy.UserEntry{Label: "alice", Secret: "aabbccddeeff00112233445566778899"}
	userBob   = teleproxy.UserEntry{Label: "bob", Secret: "00112233445566778899aabbccddeeff"}
)

func singleCfg() teleproxy.Config {
	return teleproxy.Config{
		Port:      443,
		MaskHost:  "www.microsoft.com",
		StatsPort: 9091,
		Users:     []teleproxy.UserEntry{userAlice, userBob},
	}
}

func bridgeCfg() teleproxy.Config {
	return teleproxy.Config{
		Port:       443,
		MaskHost:   "www.microsoft.com",
		StatsPort:  9091,
		SOCKS5Addr: "socks5://127.0.0.1:2080",
		Users:      []teleproxy.UserEntry{userAlice},
	}
}

func TestRenderSingleMode_Golden(t *testing.T) {
	got := singleCfg().Render()
	checkGolden(t, "single.toml", got)
}

func TestRenderBridgeMode_Golden(t *testing.T) {
	got := bridgeCfg().Render()
	checkGolden(t, "bridge.toml", got)
}

func TestRenderSingleMode_NoSOCKS5(t *testing.T) {
	got := singleCfg().Render()
	if bytes.Contains(got, []byte("socks5")) {
		t.Error("Single mode output must not contain socks5")
	}
}

func TestRenderBridgeMode_HasSOCKS5(t *testing.T) {
	got := bridgeCfg().Render()
	if !bytes.Contains(got, []byte("127.0.0.1:2080")) {
		t.Error("Bridge mode output must contain SOCKS5 address")
	}
}

func TestRenderContainsUserLabels(t *testing.T) {
	got := singleCfg().Render()
	for _, u := range []teleproxy.UserEntry{userAlice, userBob} {
		if !bytes.Contains(got, []byte(u.Label)) {
			t.Errorf("output missing label %q", u.Label)
		}
		if !bytes.Contains(got, []byte(u.Secret)) {
			t.Errorf("output missing secret for %q", u.Label)
		}
	}
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
