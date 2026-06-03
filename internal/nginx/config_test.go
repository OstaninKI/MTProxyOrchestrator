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

func TestStubRenderPublicBind(t *testing.T) {
	out := baseCfg.Render()
	if !bytes.Contains(out, []byte("0.0.0.0")) {
		t.Error("output must bind on 0.0.0.0 so external browsers can reach the stub page")
	}
}

func TestTLSStubRenderLoopbackBindWithCertificate(t *testing.T) {
	cfg := nginx.TLSStubConfig{
		ListenPort: 9443,
		ServerName: "proxy.example.com",
		StubRoot:   "/var/www/tgproxy-stub",
		CertPath:   "/etc/tgproxy/certs/proxy.example.com/cert.pem",
		KeyPath:    "/etc/tgproxy/certs/proxy.example.com/key.pem",
	}
	out := cfg.Render()
	for _, want := range []string{
		"listen 127.0.0.1:9443 ssl",
		"server_name proxy.example.com",
		"ssl_certificate     /etc/tgproxy/certs/proxy.example.com/cert.pem",
		"ssl_certificate_key /etc/tgproxy/certs/proxy.example.com/key.pem",
		"ssl_protocols TLSv1.3",
		"root /var/www/tgproxy-stub",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Fatalf("TLS stub config missing %q:\n%s", want, out)
		}
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

// PanelProxyConfig tests

var panelCfg = nginx.PanelProxyConfig{
	ListenPort:  8443,
	Domain:      "proxy.example.com",
	CertPath:    "/etc/lego/certificates/proxy.example.com.crt",
	KeyPath:     "/etc/lego/certificates/proxy.example.com.key",
	BackendAddr: "127.0.0.1:18080",
}

func TestPanelProxyConfig_UsesConfiguredPublicListenPort(t *testing.T) {
	out := panelCfg.Render()
	if !bytes.Contains(out, []byte("listen 0.0.0.0:8443 ssl")) {
		t.Error("output must listen on configured public panel TLS port")
	}
	if bytes.Contains(out, []byte("listen 0.0.0.0:443 ssl")) {
		t.Error("panel proxy must not bind 443 because Teleproxy owns it")
	}
}

func TestPanelProxyConfig_HasTLS12And13(t *testing.T) {
	out := panelCfg.Render()
	if !bytes.Contains(out, []byte("ssl_protocols TLSv1.2 TLSv1.3")) {
		t.Error("output must contain ssl_protocols TLSv1.2 TLSv1.3")
	}
}

func TestPanelProxyConfig_HasHSTS(t *testing.T) {
	out := panelCfg.Render()
	if !bytes.Contains(out, []byte(`Strict-Transport-Security "max-age=63072000; includeSubDomains; preload"`)) {
		t.Error("output must contain Strict-Transport-Security header with max-age=63072000; includeSubDomains; preload")
	}
}

func TestPanelProxyConfig_ServerTokensOff(t *testing.T) {
	out := panelCfg.Render()
	if !bytes.Contains(out, []byte("server_tokens off")) {
		t.Error("output must contain server_tokens off")
	}
}

func TestPanelProxyConfig_ProxiesToBackend(t *testing.T) {
	out := panelCfg.Render()
	if !bytes.Contains(out, []byte("proxy_pass http://127.0.0.1:18080")) {
		t.Error("output must contain proxy_pass http://127.0.0.1:18080")
	}
}

func TestPanelProxyConfig_ReplacesForwardedForWithRemoteAddr(t *testing.T) {
	out := panelCfg.Render()
	if !bytes.Contains(out, []byte("proxy_set_header X-Forwarded-For $remote_addr")) {
		t.Error("X-Forwarded-For must be replaced with the nginx peer address to prevent spoofing")
	}
	if bytes.Contains(out, []byte("$proxy_add_x_forwarded_for")) {
		t.Error("X-Forwarded-For must not append client-supplied values")
	}
}

func TestPanelProxyConfig_HasSecurityHeaders(t *testing.T) {
	out := panelCfg.Render()
	if !bytes.Contains(out, []byte("X-Frame-Options DENY")) {
		t.Error("output must contain X-Frame-Options DENY")
	}
	if !bytes.Contains(out, []byte("X-Content-Type-Options nosniff")) {
		t.Error("output must contain X-Content-Type-Options nosniff")
	}
}

func TestPanelProxyConfig_HasCSP(t *testing.T) {
	out := panelCfg.Render()
	if !bytes.Contains(out, []byte("Content-Security-Policy")) {
		t.Error("output must contain Content-Security-Policy header")
	}
	if !bytes.Contains(out, []byte("frame-ancestors 'none'")) {
		t.Error("Content-Security-Policy must include frame-ancestors 'none'")
	}
}

// ACMEChallengeConfig tests

var acmeCfg = nginx.ACMEChallengeConfig{
	WebRootDir: "/etc/tgproxy/certs/.well-known-webroot",
}

func TestACMEChallengeConfig_ContainsLocation(t *testing.T) {
	out := acmeCfg.Render()
	if !bytes.Contains(out, []byte("location /.well-known/acme-challenge/")) {
		t.Error("snippet must contain ACME challenge location block")
	}
	if !bytes.Contains(out, []byte("root /etc/tgproxy/certs/.well-known-webroot")) {
		t.Error("snippet must set root to the configured webroot dir")
	}
}

func TestACMEChallengeConfig_Golden(t *testing.T) {
	got := acmeCfg.Render()
	path := filepath.Join("testdata", "acme-challenge.conf")
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

func TestStubConfig_IncludesACMESnippetWhenSet(t *testing.T) {
	cfg := nginx.StubConfig{
		ListenPort:      80,
		ServerName:      "_",
		StubRoot:        "/var/www/tgproxy-stub",
		ACMESnippetPath: "/etc/nginx/snippets/acme-challenge.conf",
	}
	out := cfg.Render()
	if !bytes.Contains(out, []byte("include /etc/nginx/snippets/acme-challenge.conf")) {
		t.Error("stub config must include ACME snippet path when set")
	}
}

func TestStubConfig_NoACMESnippetWhenEmpty(t *testing.T) {
	out := baseCfg.Render() // baseCfg has no ACMESnippetPath
	if bytes.Contains(out, []byte("include")) {
		t.Error("stub config must not contain include directive when ACMESnippetPath is empty")
	}
}

func TestPanelProxyConfig_Golden(t *testing.T) {
	got := panelCfg.Render()
	path := filepath.Join("testdata", "panel-proxy.conf")
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
