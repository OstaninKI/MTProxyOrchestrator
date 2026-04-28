package db

import "fmt"

type migration struct {
	name string
	sql  string
}

var migrations = []migration{
	{"001_create_schema", sqlSchema},
}

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
