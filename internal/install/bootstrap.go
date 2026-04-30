package install

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
)

// BootstrapPanelDB creates or updates the initial panel state produced by the installer.
func BootstrapPanelDB(path string, bootstrap PanelBootstrap) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create panel db dir: %w", err)
	}

	database, err := db.Open(path)
	if err != nil {
		return fmt.Errorf("open panel db: %w", err)
	}
	defer database.Close()

	hash, err := panel.HashPassword(bootstrap.AdminPassword)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin bootstrap tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`INSERT INTO admin(id, login, password_hash) VALUES(1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET login=excluded.login, password_hash=excluded.password_hash, updated_at=datetime('now')`,
		bootstrap.AdminLogin, hash,
	); err != nil {
		return fmt.Errorf("upsert admin: %w", err)
	}

	var userID int64
	err = tx.QueryRow(`SELECT id FROM users WHERE label=?`, bootstrap.UserLabel).Scan(&userID)
	switch {
	case err == nil:
		if _, err := tx.Exec(
			`UPDATE users SET secret_hex=?, enabled=1, deleted_at=NULL WHERE id=?`,
			bootstrap.UserSecretHex, userID,
		); err != nil {
			return fmt.Errorf("update first user: %w", err)
		}
	case err == sql.ErrNoRows:
		if _, err := tx.Exec(
			`INSERT INTO users(label, secret_hex, enabled) VALUES(?, ?, 1)`,
			bootstrap.UserLabel, bootstrap.UserSecretHex,
		); err != nil {
			return fmt.Errorf("insert first user: %w", err)
		}
	default:
		return fmt.Errorf("lookup first user: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bootstrap tx: %w", err)
	}
	if err := chmodPanelDB(path); err != nil {
		return err
	}
	return nil
}

func chmodPanelDB(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod panel db: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		auxPath := path + suffix
		if err := os.Chmod(auxPath, 0o600); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("chmod %s: %w", auxPath, err)
		}
	}
	return nil
}
