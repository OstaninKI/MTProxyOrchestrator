package db

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// DB wraps *sql.DB.
type DB struct {
	*sql.DB
}

// Open opens or creates a SQLite database at path and runs all pending migrations.
// Pass ":memory:" for in-memory (tests).
func Open(path string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := sqldb.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		sqldb.Close()
		return nil, err
	}
	d := &DB{sqldb}
	if err := migrate(d); err != nil {
		sqldb.Close()
		return nil, err
	}
	return d, nil
}

// GetSetting returns the stored value for key, or defaultVal if absent.
func (d *DB) GetSetting(key, defaultVal string) string {
	var v string
	if err := d.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return defaultVal
	}
	return v
}

// SetSetting upserts key=value in the settings table.
func (d *DB) SetSetting(key, value string) error {
	_, err := d.Exec(
		`INSERT INTO settings(key,value,updated_at) VALUES(?,?,datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value,
	)
	return err
}
