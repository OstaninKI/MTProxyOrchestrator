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
		MSSClamp:  true,
		JA4Log:    true,
		Users:     []teleproxy.UserEntry{userAlice, userBob},
	}
}

func bridgeCfg() teleproxy.Config {
	return teleproxy.Config{
		Port:       443,
		MaskHost:   "www.microsoft.com",
		StatsPort:  9091,
		SOCKS5Addr: "socks5://127.0.0.1:2080",
		MSSClamp:   true,
		JA4Log:     true,
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

func TestRenderCustomTLSBackend(t *testing.T) {
	cfg := singleCfg()
	cfg.TLSBackend = "127.0.0.1:9443"
	got := cfg.Render()
	want := `domain = [{ name = "www.microsoft.com", backend = "127.0.0.1:9443" }]`
	if !bytes.Contains(got, []byte(want)) {
		t.Fatalf("output missing domain backend %q:\n%s", want, got)
	}
}

func TestRenderWildcardMaskWithBackend(t *testing.T) {
	cfg := singleCfg()
	cfg.WildcardMask = "*.example.com"
	cfg.TLSBackend = "proxy.example.com:443"
	got := cfg.Render()
	want := `domain = [{ name = "*.example.com", backend = "proxy.example.com:443" }]`
	if !bytes.Contains(got, []byte(want)) {
		t.Fatalf("output missing wildcard backend %q:\n%s", want, got)
	}
}

func TestRenderCanDisableMSSClamp(t *testing.T) {
	cfg := singleCfg()
	cfg.MSSClamp = false
	got := cfg.Render()
	if !bytes.Contains(got, []byte("mss_clamp = false")) {
		t.Fatalf("output must explicitly disable MSS clamp:\n%s", got)
	}
}

func TestRenderCanDisableJA4Log(t *testing.T) {
	cfg := singleCfg()
	cfg.JA4Log = false
	got := cfg.Render()
	if bytes.Contains(got, []byte("ja4_log")) {
		t.Fatalf("output must not enable JA4 logging when disabled:\n%s", got)
	}
}

func TestSOCKS5URLNormalization(t *testing.T) {
	tests := []struct {
		name          string
		socks5Addr    string
		wantRendered  string
		notWantString string
	}{
		{
			name:         "bare host:port gets socks5:// prefix",
			socks5Addr:   "127.0.0.1:1080",
			wantRendered: `socks5 = "socks5://127.0.0.1:1080"`,
		},
		{
			name:         "already-schemed socks5:// not double-prefixed",
			socks5Addr:   "socks5://127.0.0.1:2080",
			wantRendered: `socks5 = "socks5://127.0.0.1:2080"`,
			// Make sure we don't have double prefix
			notWantString: "socks5://socks5://",
		},
		{
			name:         "socks5h:// scheme unchanged",
			socks5Addr:   "socks5h://user:pass@proxy.example.com:9050",
			wantRendered: `socks5 = "socks5h://user:pass@proxy.example.com:9050"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := singleCfg()
			cfg.SOCKS5Addr = tt.socks5Addr
			got := cfg.Render()

			if !bytes.Contains(got, []byte(tt.wantRendered)) {
				t.Errorf("output missing expected %q:\n%s", tt.wantRendered, got)
			}
			if tt.notWantString != "" && bytes.Contains(got, []byte(tt.notWantString)) {
				t.Errorf("output should not contain %q but it does:\n%s", tt.notWantString, got)
			}
		})
	}
}

func TestSOCKS5EmptyAddr(t *testing.T) {
	cfg := singleCfg()
	cfg.SOCKS5Addr = ""
	got := cfg.Render()
	if bytes.Contains(got, []byte("socks5")) {
		t.Error("output with empty SOCKS5Addr must not contain socks5 directive")
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
