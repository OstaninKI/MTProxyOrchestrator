package config

import (
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type tomlConfig struct {
	Mode                string  `toml:"mode"`
	MTProtoPort         int     `toml:"mtproto_port"`
	MaskHost            string  `toml:"mask_host"`
	TLSBackend          string  `toml:"tls_backend"`
	WildcardMask        string  `toml:"wildcard_mask"`
	MSSClamp            *bool   `toml:"mss_clamp"`
	RandomPadding       *bool   `toml:"random_padding"`
	JA4Log              *bool   `toml:"ja4_log"`
	BridgeStrategy      string  `toml:"bridge_strategy"`
	LogLevel            string  `toml:"log_level"`
	TCPKeepaliveSeconds float64 `toml:"tcp_keepalive_seconds"`
	PanelPath           string  `toml:"panel_path"`
	PanelDomain         string  `toml:"panel_domain"`
	PanelCertPath       string  `toml:"panel_cert_path"`
	PanelKeyPath        string  `toml:"panel_key_path"`
	ACMEEmail           string  `toml:"acme_email"`
}

func ReadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var tc tomlConfig
	if err := toml.Unmarshal(data, &tc); err != nil {
		return Config{}, err
	}
	cfg := Default()
	if tc.Mode != "" {
		cfg.Mode = Mode(tc.Mode)
	}
	if tc.MTProtoPort > 0 {
		cfg.MTProtoPort = tc.MTProtoPort
	}
	if tc.MaskHost != "" {
		cfg.MaskHost = tc.MaskHost
	}
	cfg.TLSBackend = tc.TLSBackend
	cfg.WildcardMask = tc.WildcardMask
	if tc.MSSClamp != nil {
		cfg.MSSClamp = *tc.MSSClamp
	}
	if tc.RandomPadding != nil {
		cfg.RandomPadding = *tc.RandomPadding
	}
	if tc.JA4Log != nil {
		cfg.JA4Log = *tc.JA4Log
	}
	if tc.BridgeStrategy != "" {
		cfg.BridgeStrategy = tc.BridgeStrategy
	}
	if tc.LogLevel != "" {
		cfg.LogLevel = tc.LogLevel
	}
	if tc.TCPKeepaliveSeconds > 0 {
		cfg.TCPKeepalive = time.Duration(tc.TCPKeepaliveSeconds) * time.Second
	}
	cfg.PanelPath = tc.PanelPath
	cfg.PanelDomain = tc.PanelDomain
	cfg.PanelCertPath = tc.PanelCertPath
	cfg.PanelKeyPath = tc.PanelKeyPath
	cfg.ACMEEmail = tc.ACMEEmail
	return cfg, nil
}
