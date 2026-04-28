package panel

import (
	"fmt"
	"net/url"

	qrcode "github.com/skip2/go-qrcode"
)

// ProxyLink holds the parts needed to generate a Telegram proxy link.
type ProxyLink struct {
	Server    string // IP or hostname
	Port      int
	SecretHex string // 32 hex chars (plain MTProto secret)
	MaskHost  string // FakeTLS domain, e.g. "www.microsoft.com"
}

// TelegramURL returns the tg://proxy deep-link for this proxy.
// Secret format: "ee" + SecretHex + hex(MaskHost) — FakeTLS encoding used by Teleproxy.
func (p ProxyLink) TelegramURL() string {
	secret := buildFakeTLSSecret(p.SecretHex, p.MaskHost)
	v := url.Values{}
	v.Set("server", p.Server)
	v.Set("port", fmt.Sprintf("%d", p.Port))
	v.Set("secret", secret)
	return "tg://proxy?" + v.Encode()
}

// QRPNG returns a PNG-encoded QR code for the Telegram proxy link.
// size is the pixel dimension of the output image (e.g. 256).
func (p ProxyLink) QRPNG(size int) ([]byte, error) {
	return qrcode.Encode(p.TelegramURL(), qrcode.Medium, size)
}

// buildFakeTLSSecret encodes the secret in FakeTLS format:
// "ee" + secretHex + hex(domain bytes)
func buildFakeTLSSecret(secretHex, domain string) string {
	return "ee" + secretHex + fmt.Sprintf("%x", []byte(domain))
}
