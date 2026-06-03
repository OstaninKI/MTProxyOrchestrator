package panel_test

import (
	"fmt"
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
	if !strings.Contains(u, "secret=ee") {
		t.Errorf("URL secret must include FakeTLS ee prefix, got: %s", u)
	}
}

func TestTelegramURLSecretRandomPaddingPrefix(t *testing.T) {
	link := baseLink()
	link.RandomPadding = true
	u := link.TelegramURL()
	// Obfuscated2 transport: secret = "dd" + SecretHex (no domain, no "ee")
	expected := "secret=dd" + link.SecretHex
	if !strings.Contains(u, expected) {
		t.Errorf("URL secret must be dd+SecretHex for Obfuscated2, got: %s", u)
	}
	// Secret must not start with "ddee" (that would be the old broken combined prefix)
	if strings.Contains(u, "secret=ddee") {
		t.Errorf("Obfuscated2 secret must not start with 'ddee' (invalid combined prefix), got: %s", u)
	}
	// Domain must not appear in the secret
	if strings.Contains(u, "microsoft") {
		t.Errorf("Obfuscated2 secret must not contain mask host domain, got: %s", u)
	}
}

func TestTelegramURLFakeTLSContainsDomain(t *testing.T) {
	link := baseLink()
	// RandomPadding=false (default) → Fake-TLS: "ee" + SecretHex + hex(MaskHost)
	u := link.TelegramURL()
	if strings.Contains(u, "secret=dd") {
		t.Errorf("Fake-TLS URL must not start secret with dd, got: %s", u)
	}
	// hex("www.microsoft.com") should be embedded in the secret
	domainHex := fmt.Sprintf("%x", []byte("www.microsoft.com"))
	if !strings.Contains(u, domainHex) {
		t.Errorf("Fake-TLS secret must contain hex-encoded domain %s, got: %s", domainHex, u)
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
