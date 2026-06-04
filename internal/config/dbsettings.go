package config

import (
	"database/sql"
	"strconv"

	_ "modernc.org/sqlite"
)

type RuntimeSettings struct {
	MaskHost      string
	TLSBackend    string
	WildcardMask  string
	MSSClamp      *bool
	RandomPadding *bool
	JA4Log        *bool
	MTProtoPort   int
	PanelPath     string
	LogLevel      string
	Mode          string // "bridge" or "single"; empty means not set in DB
}

func ReadRuntimeSettings(dbPath string) (RuntimeSettings, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return RuntimeSettings{}, err
	}
	defer db.Close()

	var rs RuntimeSettings

	var maskHost string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'mask_host'`).Scan(&maskHost); err == nil {
		rs.MaskHost = maskHost
	}
	var tlsBackend string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'tls_backend'`).Scan(&tlsBackend); err == nil {
		rs.TLSBackend = tlsBackend
	}
	var wildcardMask string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'wildcard_mask'`).Scan(&wildcardMask); err == nil {
		rs.WildcardMask = wildcardMask
	}
	if v, ok := readBoolSetting(db, "mss_clamp"); ok {
		rs.MSSClamp = &v
	}
	if v, ok := readBoolSetting(db, "random_padding"); ok {
		rs.RandomPadding = &v
	}
	if v, ok := readBoolSetting(db, "ja4_log"); ok {
		rs.JA4Log = &v
	}

	var portStr string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'mtproto_port'`).Scan(&portStr); err == nil {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			rs.MTProtoPort = p
		}
	}

	var panelPath string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'panel_path'`).Scan(&panelPath); err == nil {
		rs.PanelPath = panelPath
	}

	var logLevel string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'log_level'`).Scan(&logLevel); err == nil {
		rs.LogLevel = logLevel
	}

	var bridgeMode string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'bridge_mode'`).Scan(&bridgeMode); err == nil {
		rs.Mode = bridgeMode
	}

	return rs, nil
}

func readBoolSetting(db *sql.DB, key string) (bool, bool) {
	var raw string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&raw); err != nil {
		return false, false
	}
	switch raw {
	case "1", "true", "on", "yes":
		return true, true
	case "0", "false", "off", "no":
		return false, true
	default:
		return false, false
	}
}

func (rs RuntimeSettings) MergeInto(cfg Config) Config {
	if rs.MaskHost != "" {
		cfg.MaskHost = rs.MaskHost
	}
	if rs.TLSBackend != "" {
		cfg.TLSBackend = rs.TLSBackend
	}
	if rs.WildcardMask != "" {
		cfg.WildcardMask = rs.WildcardMask
	}
	if rs.MSSClamp != nil {
		cfg.MSSClamp = *rs.MSSClamp
	}
	if rs.RandomPadding != nil {
		cfg.RandomPadding = *rs.RandomPadding
	}
	if rs.JA4Log != nil {
		cfg.JA4Log = *rs.JA4Log
	}
	if rs.MTProtoPort > 0 {
		cfg.MTProtoPort = rs.MTProtoPort
	}
	if rs.PanelPath != "" {
		cfg.PanelPath = rs.PanelPath
	}
	if rs.LogLevel != "" {
		cfg.LogLevel = rs.LogLevel
	}
	if rs.Mode != "" {
		cfg.Mode = Mode(rs.Mode)
	}
	return cfg
}
