package db

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"
)

// DB wraps *sql.DB.
type DB struct {
	*sql.DB
}

// ImmediateTx is a SQLite transaction acquired with BEGIN IMMEDIATE so that
// concurrent writers serialize on the database. Use Commit or Rollback exactly
// like *sql.Tx; the underlying connection is released to the pool on either.
type ImmediateTx struct {
	conn      *sql.Conn
	committed bool
}

// BeginImmediate acquires a dedicated connection and opens a SQLite
// transaction with BEGIN IMMEDIATE, so the writer lock is taken up-front.
// SQLite supports a single writer at a time; with WAL journal mode (set in
// Open), readers proceed concurrently.
func (d *DB) BeginImmediate(ctx context.Context) (*ImmediateTx, error) {
	conn, err := d.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	// Allow up to 30 s for the write lock. Covers slow operations (e.g. bcrypt
	// comparisons) held by a concurrent writer before it can commit.
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout=30000`); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		conn.Close()
		return nil, err
	}
	return &ImmediateTx{conn: conn}, nil
}

// ExecContext runs a statement on the transaction's dedicated connection.
func (t *ImmediateTx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.conn.ExecContext(ctx, q, args...)
}

// QueryRowContext runs a single-row query on the transaction's connection.
func (t *ImmediateTx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return t.conn.QueryRowContext(ctx, q, args...)
}

// Commit finalizes the transaction.
func (t *ImmediateTx) Commit(ctx context.Context) error {
	if t.committed {
		return nil
	}
	_, err := t.conn.ExecContext(ctx, `COMMIT`)
	t.committed = true
	closeErr := t.conn.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// Rollback aborts the transaction. Safe to call after Commit.
func (t *ImmediateTx) Rollback(ctx context.Context) error {
	if t.committed {
		return nil
	}
	_, err := t.conn.ExecContext(ctx, `ROLLBACK`)
	t.committed = true
	closeErr := t.conn.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// Open opens or creates a SQLite database at path and runs all pending migrations.
// Pass ":memory:" for in-memory (tests).
func Open(path string) (*DB, error) {
	// busy_timeout is set via the DSN, not an Exec: modernc applies DSN
	// pragmas to every pooled connection, whereas `db.Exec("PRAGMA ...")` only
	// touches whichever connection the pool happens to hand that call. Without
	// the DSN form, ordinary writes (settings, metrics, retention) hit
	// "database is locked" under contention because they never inherit the
	// busy_timeout that BeginImmediate sets per-connection.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// In-memory databases are per-connection in SQLite. Pin to a single
	// connection so multiple goroutines (and the BeginImmediate connection
	// pool dance) all see the same schema and rows.
	if path == ":memory:" {
		sqldb.SetMaxOpenConns(1)
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
