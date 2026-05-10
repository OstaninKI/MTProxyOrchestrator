// Package totp implements admin-facing TOTP 2FA helpers: secret generation,
// code validation, and one-shot recovery codes.
package totp

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// Issuer is the issuer label embedded in the otpauth URL and shown by
// authenticator apps next to the entry.
const Issuer = "tgproxy-panel"

// bcryptCost matches the panel's password baseline (CLAUDE.md).
const bcryptCost = 12

// recoveryCodeBytes is the random byte length for each recovery code before
// hex encoding; the user-visible code is twice this length.
const recoveryCodeBytes = 5

// GenerateSecret returns a new base32 TOTP secret and the matching otpauth URL.
// The account is shown in authenticator apps to identify the entry.
func GenerateSecret(account string) (secret, otpauthURL string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      Issuer,
		AccountName: account,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// Validate reports whether code is valid for secret with the default skew.
func Validate(secret, code string) bool {
	code = strings.TrimSpace(code)
	if secret == "" || code == "" {
		return false
	}
	if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret)); err != nil {
		return false
	}
	return totp.Validate(code, secret)
}

// GenerateRecoveryCodes returns n plaintext codes (shown to the user once) and
// their bcrypt hashes (to be persisted).
func GenerateRecoveryCodes(n int) (plaintext []string, hashes []string, err error) {
	plaintext = make([]string, 0, n)
	hashes = make([]string, 0, n)
	for i := 0; i < n; i++ {
		buf := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(buf); err != nil {
			return nil, nil, err
		}
		code := hex.EncodeToString(buf)
		h, err := bcrypt.GenerateFromPassword([]byte(code), bcryptCost)
		if err != nil {
			return nil, nil, err
		}
		plaintext = append(plaintext, code)
		hashes = append(hashes, string(h))
	}
	return plaintext, hashes, nil
}

// EncodeRecoveryHashes serialises the hash list for column storage.
func EncodeRecoveryHashes(hashes []string) (string, error) {
	if len(hashes) == 0 {
		return "", nil
	}
	b, err := json.Marshal(hashes)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DecodeRecoveryHashes returns the stored hashes; an empty payload yields nil.
func DecodeRecoveryHashes(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ConsumeRecoveryCode looks up the admin's recovery codes, removes the matching
// hash, and persists the trimmed list. Returns ok=true when a hash matched.
//
// The whole load-check-save sequence runs inside a SQLite BEGIN IMMEDIATE
// transaction so two concurrent requests with the same plaintext code cannot
// both observe the matching hash and both write the trimmed list — the second
// caller blocks on the writer lock and re-reads the already-trimmed JSON.
func ConsumeRecoveryCode(ctx context.Context, d *db.DB, adminID int64, plaintext string) (bool, error) {
	plaintext = strings.TrimSpace(plaintext)
	if plaintext == "" {
		return false, nil
	}
	conn, err := d.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	// Ensure BEGIN IMMEDIATE waits instead of failing fast when another
	// writer holds the lock. Scoped to this connection.
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout=5000`); err != nil {
		return false, err
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(ctx, `ROLLBACK`) //nolint:errcheck
		}
	}()
	var stored string
	if err := conn.QueryRowContext(ctx, `SELECT totp_recovery_codes FROM admin WHERE id=?`, adminID).Scan(&stored); err != nil {
		return false, err
	}
	hashes, err := DecodeRecoveryHashes(stored)
	if err != nil {
		return false, err
	}
	matchIdx := -1
	for i, h := range hashes {
		if bcrypt.CompareHashAndPassword([]byte(h), []byte(plaintext)) == nil {
			matchIdx = i
			break
		}
	}
	if matchIdx < 0 {
		if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
			return false, err
		}
		committed = true
		return false, nil
	}
	hashes = append(hashes[:matchIdx], hashes[matchIdx+1:]...)
	encoded, err := EncodeRecoveryHashes(hashes)
	if err != nil {
		return false, err
	}
	if _, err := conn.ExecContext(ctx, `UPDATE admin SET totp_recovery_codes=? WHERE id=?`, encoded, adminID); err != nil {
		return false, fmt.Errorf("update recovery codes: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}
