package config

import "time"

type Mode string

const (
	ModeSingle Mode = "single"
	ModeBridge Mode = "bridge"
)

// Config holds all tgproxy global settings.
type Config struct {
	Mode           Mode
	MTProtoPort    int
	MaskHost       string
	BridgeStrategy string
	LogLevel       string
	TCPKeepalive   time.Duration
	// PanelPath is set at install time to a random value (e.g. "/p-a8f3k2x9/").
	// It has no static default because it must be unique per installation.
	PanelPath string
	// PanelDomain and certificate paths enable the public HTTPS nginx proxy for
	// the panel. Empty values keep the panel backend loopback-only.
	PanelDomain   string
	PanelCertPath string
	PanelKeyPath  string
	// ACMEEmail enables Let's Encrypt for the panel certificate when non-empty.
	// The cert is obtained during install and renewed by tgproxy-panel automatically.
	ACMEEmail string

	// TelegramBotToken is reserved for v2 and rendered as a commented section.
	TelegramBotToken string
}

// Default returns a Config with all spec-defined defaults applied.
func Default() Config {
	return Config{
		Mode:           ModeSingle,
		MTProtoPort:    443,
		MaskHost:       "www.microsoft.com",
		BridgeStrategy: "urltest",
		LogLevel:       "info",
		TCPKeepalive:   60 * time.Second,
	}
}
