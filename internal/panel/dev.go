package panel

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"golang.org/x/crypto/bcrypt"
)

// noopBridgeExecutor implements bridge.Executor with no-op operations.
// Used in dev mode so bridge enable/disable handlers succeed without touching the OS.
type noopBridgeExecutor struct{}

func (noopBridgeExecutor) WriteFile(_ string, _ []byte, _ os.FileMode) error { return nil }
func (noopBridgeExecutor) Download(_, _, _ string) error                     { return nil }
func (noopBridgeExecutor) EnableService(_ string) error                      { return nil }
func (noopBridgeExecutor) StartService(_ string) error                       { return nil }
func (noopBridgeExecutor) StopService(_ string) error                        { return nil }
func (noopBridgeExecutor) DisableService(_ string) error                     { return nil }
func (noopBridgeExecutor) ReloadService(_ string) error                      { return nil }
func (noopBridgeExecutor) ServiceActive(_ string) (bool, error)              { return false, nil }

// SeedDevData populates d with demo admin, users, traffic samples, and settings.
// Must be called exactly once on a freshly opened DB; a second call on the same DB
// returns an error (duplicate admin row). Migrations are applied by db.Open.
func SeedDevData(d *db.DB) error {
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), 12)
	if err != nil {
		return err
	}
	if _, err := d.Exec(
		`INSERT INTO admin(id, login, password_hash) VALUES(1,'admin',?)`,
		string(hash),
	); err != nil {
		return err
	}

	users := []struct{ label string }{
		{"alice"},
		{"bob"},
		{"charlie"},
		{"david"},
		{"eve"},
	}
	for _, u := range users {
		secret, err := devRandomHex(32)
		if err != nil {
			return err
		}
		if _, err := d.Exec(
			`INSERT INTO users(label, secret_hex, enabled) VALUES(?,?,1)`,
			u.label, secret,
		); err != nil {
			return err
		}
	}
	// Disabled user
	if _, err := d.Exec(`UPDATE users SET enabled=0 WHERE label='eve'`); err != nil {
		return err
	}
	// Quota-suspended user (quota exhausted)
	if _, err := d.Exec(
		`UPDATE users SET quota_bytes=1073741824, quota_used_bytes=1073741824, quota_suspended=1 WHERE label='david'`,
	); err != nil {
		return err
	}

	// Traffic samples: 72 hourly rows per user over last 3 days
	now := time.Now().Unix()
	for i := 0; i < 72; i++ {
		ts := now - int64(i)*3600
		_, _ = d.Exec(
			`INSERT INTO traffic_samples(user_label,ts,bytes_in,bytes_out,connections) VALUES(?,?,?,?,?)`,
			"alice", ts, int64(1<<20)*(1+int64(i%5)), int64(512<<10)*(1+int64(i%3)), 2+i%4,
		)
		_, _ = d.Exec(
			`INSERT INTO traffic_samples(user_label,ts,bytes_in,bytes_out,connections) VALUES(?,?,?,?,?)`,
			"bob", ts, int64(512<<10)*(1+int64(i%3)), int64(256<<10)*(1+int64(i%2)), 1+i%2,
		)
	}

	// Settings
	settings := [][2]string{
		{"mask_host", "www.example.com"},
		{"mtproto_port", "443"},
		{"server_ip", "127.0.0.1"},
	}
	for _, kv := range settings {
		_, _ = d.Exec(
			`INSERT OR REPLACE INTO settings(key,value) VALUES(?,?)`,
			kv[0], kv[1],
		)
	}

	return nil
}

// devRandomHex returns n random bytes encoded as a lowercase hex string.
func devRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
