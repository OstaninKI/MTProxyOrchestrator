package panel_test

import (
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
)

func baseLink() panel.ProxyLink {
	return panel.ProxyLink{
		Server:    "198.51.100.1",
		Port:      443,
		SecretHex: "aabbccddeeff00112233445566778899",
		MaskHost:  "www.microsoft.com",
	}
}

func TestTelegramURLScheme(t *testing.T) {
	u := baseLink().TelegramURL()
	if !strings.HasPrefix(u, "tg://proxy?") {
		t.Errorf("URL must start with tg://proxy?, got: %s", u)
	}
}

func TestTelegramURLContainsServer(t *testing.T) {
	u := baseLink().TelegramURL()
	if !strings.Contains(u, "server=198.51.100.1") {
		t.Errorf("URL must contain server param, got: %s", u)
	}
}

func TestTelegramURLSecretFakeTLSPrefix(t *testing.T) {
	u := baseLink().TelegramURL()
	if !strings.Contains(u, "ee") {
		t.Errorf("URL secret must include FakeTLS ee prefix, got: %s", u)
	}
}

func TestQRPNGNotEmpty(t *testing.T) {
	png, err := baseLink().QRPNG(256)
	if err != nil {
		t.Fatal(err)
	}
	if len(png) == 0 {
		t.Error("QR PNG must not be empty")
	}
	if len(png) < 4 || png[0] != 0x89 || png[1] != 0x50 {
		t.Error("output does not look like a PNG")
	}
}

func TestProxyLinkUsesDomainAsServer(t *testing.T) {
	link := panel.ProxyLink{
		Server:    "proxy.example.com",
		Port:      443,
		SecretHex: "aabbccddeeff00112233445566778899",
		MaskHost:  "www.microsoft.com",
	}
	u := link.TelegramURL()
	if !strings.Contains(u, "server=proxy.example.com") {
		t.Errorf("URL must contain server=proxy.example.com, got: %s", u)
	}
}

func TestProxyLinkUsesIPAsServer(t *testing.T) {
	link := panel.ProxyLink{
		Server:    "198.51.100.1",
		Port:      443,
		SecretHex: "aabbccddeeff00112233445566778899",
		MaskHost:  "www.microsoft.com",
	}
	u := link.TelegramURL()
	if !strings.Contains(u, "server=198.51.100.1") {
		t.Errorf("URL must contain server=198.51.100.1, got: %s", u)
	}
}

func TestQRPNGDifferentLinks(t *testing.T) {
	a := baseLink()
	b := baseLink()
	b.Server = "10.0.0.1"
	pngA, err := a.QRPNG(128)
	if err != nil {
		t.Fatal(err)
	}
	pngB, err := b.QRPNG(128)
	if err != nil {
		t.Fatal(err)
	}
	if string(pngA) == string(pngB) {
		t.Error("different links should produce different QR codes")
	}
}
