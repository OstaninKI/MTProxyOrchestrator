package bridge_test

import (
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
