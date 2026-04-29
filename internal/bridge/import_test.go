package bridge_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
)

const validVLESS = "vless://a3b4c5d6-e7f8-4a2b-9c0d-1e2f3a4b5c6d@1.2.3.4:443" +
	"?security=reality&sni=example.com&pbk=ABC123pubkey==&sid=abcd1234&flow=xtls-rprx-vision&type=tcp" +
	"#my-node"

func TestImportVLESSValid(t *testing.T) {
	n, err := bridge.ImportVLESS(validVLESS)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.UUID != "a3b4c5d6-e7f8-4a2b-9c0d-1e2f3a4b5c6d" {
		t.Errorf("uuid: got %q", n.UUID)
	}
	if n.Host != "1.2.3.4" {
		t.Errorf("host: got %q", n.Host)
	}
	if n.Port != 443 {
		t.Errorf("port: got %d", n.Port)
	}
	if n.SNI != "example.com" {
		t.Errorf("sni: got %q", n.SNI)
	}
	if n.PublicKey != "ABC123pubkey==" {
		t.Errorf("public_key: got %q", n.PublicKey)
	}
	if n.ShortID != "abcd1234" {
		t.Errorf("short_id: got %q", n.ShortID)
	}
	if n.Flow != "xtls-rprx-vision" {
		t.Errorf("flow: got %q", n.Flow)
	}
	if n.Tag != "my-node" {
		t.Errorf("tag from fragment: got %q", n.Tag)
	}
	if !n.Enabled {
		t.Error("node should be enabled by default")
	}
	if n.Type != bridge.NodeTypeVLESSReality {
		t.Errorf("type: got %q", n.Type)
	}
}

func TestImportVLESSNoFragment_TagFromHostPort(t *testing.T) {
	raw := "vless://uuid@10.0.0.1:8443?security=reality&sni=sni.test&pbk=pk&sid=0011"
	n, err := bridge.ImportVLESS(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Tag != "10.0.0.1:8443" {
		t.Errorf("want tag=10.0.0.1:8443, got %q", n.Tag)
	}
}

func TestImportVLESSEmptyShortID(t *testing.T) {
	raw := "vless://uuid@10.0.0.1:443?security=reality&sni=s.example&pbk=pk&sid="
	n, err := bridge.ImportVLESS(raw)
	if err != nil {
		t.Fatalf("empty short_id should be allowed: %v", err)
	}
	if n.ShortID != "" {
		t.Errorf("want empty short_id, got %q", n.ShortID)
	}
}

func TestImportVLESSNoFlow(t *testing.T) {
	raw := "vless://uuid@10.0.0.1:443?security=reality&sni=s.example&pbk=pk&sid=abc"
	n, err := bridge.ImportVLESS(raw)
	if err != nil {
		t.Fatalf("no flow should be allowed: %v", err)
	}
	if n.Flow != "" {
		t.Errorf("want empty flow, got %q", n.Flow)
	}
}

var invalidURLs = []struct {
	name   string
	raw    string
	errSub string
}{
	{
		name:   "wrong scheme",
		raw:    "trojan://uuid@1.2.3.4:443?security=reality&sni=s&pbk=pk&sid=sid",
		errSub: "vless://",
	},
	{
		name:   "missing uuid",
		raw:    "vless://@1.2.3.4:443?security=reality&sni=s&pbk=pk&sid=sid",
		errSub: "uuid",
	},
	{
		name:   "missing host",
		raw:    "vless://uuid@:443?security=reality&sni=s&pbk=pk&sid=sid",
		errSub: "host",
	},
	{
		name:   "missing port",
		raw:    "vless://uuid@1.2.3.4?security=reality&sni=s&pbk=pk&sid=sid",
		errSub: "port",
	},
	{
		name:   "invalid port",
		raw:    "vless://uuid@1.2.3.4:99999?security=reality&sni=s&pbk=pk&sid=sid",
		errSub: "port",
	},
	{
		name:   "security not reality",
		raw:    "vless://uuid@1.2.3.4:443?security=tls&sni=s&pbk=pk&sid=sid",
		errSub: "reality",
	},
	{
		name:   "missing sni",
		raw:    "vless://uuid@1.2.3.4:443?security=reality&pbk=pk&sid=sid",
		errSub: "sni",
	},
	{
		name:   "missing pbk",
		raw:    "vless://uuid@1.2.3.4:443?security=reality&sni=s&sid=sid",
		errSub: "pbk",
	},
	{
		name:   "missing sid parameter",
		raw:    "vless://uuid@1.2.3.4:443?security=reality&sni=s&pbk=pk",
		errSub: "sid",
	},
}

func TestImportVLESSInvalid(t *testing.T) {
	for _, tc := range invalidURLs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := bridge.ImportVLESS(tc.raw)
			if err == nil {
				t.Fatalf("expected error for %q", tc.name)
			}
			if !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("error %q should mention %q", err.Error(), tc.errSub)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Trojan
// ---------------------------------------------------------------------------

func TestImportTrojanValid(t *testing.T) {
	raw := "trojan://mysecretpass@10.0.0.1:443?sni=trojan.example.com&security=tls&type=tcp#trojan-node"
	n, err := bridge.ImportTrojan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Type != bridge.NodeTypeTrojan {
		t.Errorf("type: got %q, want %q", n.Type, bridge.NodeTypeTrojan)
	}
	if n.Password != "mysecretpass" {
		t.Errorf("password: got %q", n.Password)
	}
	if n.Host != "10.0.0.1" {
		t.Errorf("host: got %q", n.Host)
	}
	if n.Port != 443 {
		t.Errorf("port: got %d", n.Port)
	}
	if n.SNI != "trojan.example.com" {
		t.Errorf("sni: got %q", n.SNI)
	}
	if n.Tag != "trojan-node" {
		t.Errorf("tag: got %q", n.Tag)
	}
	if !n.Enabled {
		t.Error("node should be enabled by default")
	}
}

func TestImportTrojanMissingSNI(t *testing.T) {
	raw := "trojan://pass@10.0.0.1:443"
	_, err := bridge.ImportTrojan(raw)
	if err == nil {
		t.Fatal("expected error for missing sni")
	}
	if !strings.Contains(err.Error(), "sni") {
		t.Errorf("error should mention sni: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Shadowsocks
// ---------------------------------------------------------------------------

func TestImportShadowsocksValid(t *testing.T) {
	// SIP002: ss://base64(method:password)@host:port#tag
	userinfo := base64.StdEncoding.EncodeToString([]byte("2022-blake3-aes-128-gcm:sspassword"))
	raw := "ss://" + userinfo + "@5.6.7.8:8388#ss-node"
	n, err := bridge.ImportShadowsocks(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Type != bridge.NodeTypeShadowsocks {
		t.Errorf("type: got %q, want %q", n.Type, bridge.NodeTypeShadowsocks)
	}
	if n.Method != "2022-blake3-aes-128-gcm" {
		t.Errorf("method: got %q", n.Method)
	}
	if n.Password != "sspassword" {
		t.Errorf("password: got %q", n.Password)
	}
	if n.Host != "5.6.7.8" {
		t.Errorf("host: got %q", n.Host)
	}
	if n.Port != 8388 {
		t.Errorf("port: got %d", n.Port)
	}
	if n.Tag != "ss-node" {
		t.Errorf("tag: got %q", n.Tag)
	}
}

// ---------------------------------------------------------------------------
// Hysteria2
// ---------------------------------------------------------------------------

func TestImportHysteria2Valid(t *testing.T) {
	for _, scheme := range []string{"hysteria2", "hy2"} {
		t.Run(scheme, func(t *testing.T) {
			raw := scheme + "://hy2pass@9.10.11.12:443?sni=hy2.example.com#hy2-node"
			n, err := bridge.ImportHysteria2(raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n.Type != bridge.NodeTypeHysteria2 {
				t.Errorf("type: got %q, want %q", n.Type, bridge.NodeTypeHysteria2)
			}
			if n.Password != "hy2pass" {
				t.Errorf("password: got %q", n.Password)
			}
			if n.SNI != "hy2.example.com" {
				t.Errorf("sni: got %q", n.SNI)
			}
			if n.Tag != "hy2-node" {
				t.Errorf("tag: got %q", n.Tag)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TUIC
// ---------------------------------------------------------------------------

func TestImportTUICValid(t *testing.T) {
	raw := "tuic://aaaabbbb-cccc-dddd-eeee-ffffffffffff:tuicpass@13.14.15.16:443?sni=tuic.example.com&congestion_control=bbr#tuic-node"
	n, err := bridge.ImportTUIC(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Type != bridge.NodeTypeTUIC {
		t.Errorf("type: got %q, want %q", n.Type, bridge.NodeTypeTUIC)
	}
	if n.UUID != "aaaabbbb-cccc-dddd-eeee-ffffffffffff" {
		t.Errorf("uuid: got %q", n.UUID)
	}
	if n.Password != "tuicpass" {
		t.Errorf("password: got %q", n.Password)
	}
	if n.SNI != "tuic.example.com" {
		t.Errorf("sni: got %q", n.SNI)
	}
	if n.CongestionControl != "bbr" {
		t.Errorf("congestion_control: got %q", n.CongestionControl)
	}
	if n.Tag != "tuic-node" {
		t.Errorf("tag: got %q", n.Tag)
	}
}

func TestImportTUICDefaultCongestionControl(t *testing.T) {
	raw := "tuic://uuid:pass@1.2.3.4:443?sni=s.example.com"
	n, err := bridge.ImportTUIC(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.CongestionControl != "bbr" {
		t.Errorf("want default congestion_control=bbr, got %q", n.CongestionControl)
	}
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

func TestImportDispatch(t *testing.T) {
	cases := []struct {
		name     string
		rawURL   string
		wantType bridge.NodeType
	}{
		{
			name:     "vless",
			rawURL:   validVLESS,
			wantType: bridge.NodeTypeVLESSReality,
		},
		{
			name:     "trojan",
			rawURL:   "trojan://pass@1.2.3.4:443?sni=s.example.com#t",
			wantType: bridge.NodeTypeTrojan,
		},
		{
			name:     "hysteria2",
			rawURL:   "hysteria2://pass@1.2.3.4:443?sni=s.example.com#h",
			wantType: bridge.NodeTypeHysteria2,
		},
		{
			name:     "hy2",
			rawURL:   "hy2://pass@1.2.3.4:443?sni=s.example.com#h",
			wantType: bridge.NodeTypeHysteria2,
		},
		{
			name:     "tuic",
			rawURL:   "tuic://uuid:pass@1.2.3.4:443?sni=s.example.com#t",
			wantType: bridge.NodeTypeTUIC,
		},
		{
			// ss://base64(method:password)@host:port — base64("chacha20-ietf-poly1305:pass")
			name:     "ss",
			rawURL:   "ss://" + base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pass")) + "@1.2.3.4:8388#s",
			wantType: bridge.NodeTypeShadowsocks,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := bridge.Import(tc.rawURL)
			if err != nil {
				t.Fatalf("Import(%q): unexpected error: %v", tc.rawURL, err)
			}
			if n.Type != tc.wantType {
				t.Errorf("type: got %q, want %q", n.Type, tc.wantType)
			}
		})
	}
}

func TestImportDispatchUnsupported(t *testing.T) {
	_, err := bridge.Import("http://example.com")
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention unsupported: %v", err)
	}
}
