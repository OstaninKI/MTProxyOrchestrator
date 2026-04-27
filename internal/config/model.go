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
	PanelPath      string

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
