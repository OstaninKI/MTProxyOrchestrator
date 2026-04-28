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
