package config

import (
	"database/sql"
	"strconv"

	_ "modernc.org/sqlite"
)

type RuntimeSettings struct {
	MaskHost    string
	MTProtoPort int
	PanelPath   string
	LogLevel    string
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

	return rs, nil
}

func (rs RuntimeSettings) MergeInto(cfg Config) Config {
	if rs.MaskHost != "" {
		cfg.MaskHost = rs.MaskHost
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
	return cfg
}
