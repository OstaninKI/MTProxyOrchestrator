package db

import "fmt"

type migration struct {
	name string
	sql  string
}

var migrations = []migration{
	{"001_create_schema", sqlSchema},
	{"002_traffic_schema", sqlTrafficSchema},
	{"003_cert_schema", sqlCertSchema},
	{"004_settings", sqlSettingsSchema},
	{"005_sessions_idle_timeout", `ALTER TABLE sessions ADD COLUMN last_seen_at DATETIME;`},
	{"006_user_quotas", sqlUserQuotas},
	{"007_admin_totp", sqlAdminTOTP},
}

const sqlAdminTOTP = `
ALTER TABLE admin ADD COLUMN totp_secret TEXT NOT NULL DEFAULT '';
ALTER TABLE admin ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE admin ADD COLUMN totp_recovery_codes TEXT NOT NULL DEFAULT '';
`

const sqlUserQuotas = `
ALTER TABLE users ADD COLUMN quota_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN quota_period TEXT NOT NULL DEFAULT 'monthly';
ALTER TABLE users ADD COLUMN quota_warn_pct INTEGER NOT NULL DEFAULT 80;
ALTER TABLE users ADD COLUMN quota_suspended INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN quota_period_start INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN quota_used_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN quota_warned INTEGER NOT NULL DEFAULT 0;
`

func migrate(d *DB) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS migrations (
		name TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}
	for _, m := range migrations {
		var count int
		if err := d.QueryRow(`SELECT COUNT(*) FROM migrations WHERE name=?`, m.name).Scan(&count); err != nil {
			return fmt.Errorf("check migration %s: %w", m.name, err)
		}
		if count > 0 {
			continue
		}
		if _, err := d.Exec(m.sql); err != nil {
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
		if _, err := d.Exec(`INSERT INTO migrations(name) VALUES(?)`, m.name); err != nil {
			return fmt.Errorf("record migration %s: %w", m.name, err)
		}
	}
	return nil
}

const sqlTrafficSchema = `
CREATE TABLE IF NOT EXISTS traffic_samples (
    id          INTEGER PRIMARY KEY,
    user_label  TEXT    NOT NULL,
    ts          INTEGER NOT NULL,
    bytes_in    INTEGER NOT NULL,
    bytes_out   INTEGER NOT NULL,
    connections INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_user_time ON traffic_samples(user_label, ts);

CREATE TABLE IF NOT EXISTS traffic_hourly (
    id          INTEGER PRIMARY KEY,
    user_label  TEXT    NOT NULL,
    hour_ts     INTEGER NOT NULL,
    bytes_in    INTEGER NOT NULL,
    bytes_out   INTEGER NOT NULL,
    connections INTEGER NOT NULL,
    UNIQUE(user_label, hour_ts)
);

CREATE TABLE IF NOT EXISTS traffic_daily (
    id          INTEGER PRIMARY KEY,
    user_label  TEXT    NOT NULL,
    day_ts      INTEGER NOT NULL,
    bytes_in    INTEGER NOT NULL,
    bytes_out   INTEGER NOT NULL,
    connections INTEGER NOT NULL,
    UNIQUE(user_label, day_ts)
);
`

const sqlCertSchema = `
CREATE TABLE IF NOT EXISTS cert_renewals (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    domain      TEXT    NOT NULL,
    success     INTEGER NOT NULL DEFAULT 0,
    error_msg   TEXT    NOT NULL DEFAULT '',
    attempted_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
`

const sqlSettingsSchema = `
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
`

const sqlSchema = `
CREATE TABLE IF NOT EXISTS admin (
	id            INTEGER PRIMARY KEY CHECK (id = 1),
	login         TEXT    NOT NULL,
	password_hash TEXT    NOT NULL,
	created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT     PRIMARY KEY,
	admin_id   INTEGER  NOT NULL REFERENCES admin(id),
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	expires_at DATETIME NOT NULL,
	ip         TEXT     NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS users (
	id         INTEGER  PRIMARY KEY AUTOINCREMENT,
	label      TEXT     NOT NULL,
	secret_hex TEXT     NOT NULL,
	enabled    INTEGER  NOT NULL DEFAULT 1,
	deleted_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	rotated_at DATETIME,
	UNIQUE(label) ON CONFLICT FAIL
);

CREATE TABLE IF NOT EXISTS audit_log (
	id         INTEGER  PRIMARY KEY AUTOINCREMENT,
	admin_id   INTEGER  REFERENCES admin(id),
	action     TEXT     NOT NULL,
	target     TEXT     NOT NULL DEFAULT '',
	detail     TEXT     NOT NULL DEFAULT '',
	ip         TEXT     NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
`
