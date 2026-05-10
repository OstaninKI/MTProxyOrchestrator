package panel

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/secrets"
)

// maxActiveUsers mirrors Teleproxy's 16-secret limit.
const maxActiveUsers = 16

// formatBytes renders a byte count with binary units (KB = 1024 B).
// One decimal place is shown for KB and above when the value is below 10
// in its unit; otherwise the value is rounded to integer.
func formatBytes(n uint64) string {
	const k = 1024
	if n < k {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	val := float64(n) / k
	idx := 0
	for val >= k && idx < len(units)-1 {
		val /= k
		idx++
	}
	if val < 10 {
		return fmt.Sprintf("%.1f %s", val, units[idx])
	}
	return fmt.Sprintf("%.0f %s", val, units[idx])
}

// quotaPct returns the integer percentage of used/total, capped at 100.
// When total is zero (unlimited), returns 0.
func quotaPct(used, total uint64) int {
	if total == 0 {
		return 0
	}
	p := (used * 100) / total
	if p > 100 {
		return 100
	}
	return int(p)
}

// nextResetIn returns a human readable countdown to the next quota period
// rollover. Returns "" for unlimited quota (period == "" treated by caller),
// "—" when periodStart is zero, and "rollover pending" when the period
// already elapsed (rollover not yet processed).
func nextResetIn(periodStart int64, period string, now time.Time) string {
	if periodStart == 0 {
		return "—"
	}
	var dur time.Duration
	switch period {
	case "daily":
		dur = 24 * time.Hour
	case "weekly":
		dur = 7 * 24 * time.Hour
	case "monthly":
		dur = 30 * 24 * time.Hour
	default:
		return "—"
	}
	end := time.Unix(periodStart, 0).Add(dur)
	left := end.Sub(now)
	if left <= 0 {
		return "rollover pending"
	}
	days := int(left / (24 * time.Hour))
	hours := int((left % (24 * time.Hour)) / time.Hour)
	mins := int((left % time.Hour) / time.Minute)
	if days > 0 {
		return fmt.Sprintf("resets in %dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("resets in %dh %dm", hours, mins)
	}
	return fmt.Sprintf("resets in %dm", mins)
}

// UserRow is one row from the users table.
type UserRow struct {
	ID        int64
	Label     string
	SecretHex string
	Enabled   bool
	CreatedAt time.Time
	RotatedAt *time.Time
	DeletedAt *time.Time

	QuotaBytes       int64
	QuotaPeriod      string
	QuotaWarnPct     int
	QuotaSuspended   bool
	QuotaPeriodStart int64
	QuotaUsedBytes   int64
	QuotaWarned      bool
}

// UserRepo wraps DB operations for users.
type UserRepo struct {
	DB *db.DB
}

// List returns all non-deleted users ordered by creation time.
func (r UserRepo) List() ([]UserRow, error) {
	rows, err := r.DB.Query(
		`SELECT id, label, secret_hex, enabled, created_at, rotated_at,
		        quota_bytes, quota_period, quota_warn_pct, quota_suspended,
		        quota_period_start, quota_used_bytes, quota_warned
		 FROM users WHERE deleted_at IS NULL ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserRow
	for rows.Next() {
		var u UserRow
		var rotated sql.NullString
		var created string
		if err := rows.Scan(&u.ID, &u.Label, &u.SecretHex, &u.Enabled, &created, &rotated,
			&u.QuotaBytes, &u.QuotaPeriod, &u.QuotaWarnPct, &u.QuotaSuspended,
			&u.QuotaPeriodStart, &u.QuotaUsedBytes, &u.QuotaWarned); err != nil {
			return nil, err
		}
		u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", created)
		if rotated.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", rotated.String)
			u.RotatedAt = &t
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetQuota updates quota configuration for a user. Resets warned/used/period_start
// only if quota_period changed or period_start is zero.
func (r UserRepo) SetQuota(id int64, bytes int64, period string, warnPct int, nowTS int64) error {
	if period != "daily" && period != "weekly" && period != "monthly" {
		return fmt.Errorf("invalid period %q", period)
	}
	if warnPct < 0 || warnPct > 100 {
		return fmt.Errorf("warn pct out of range")
	}
	if bytes < 0 {
		return fmt.Errorf("quota bytes negative")
	}
	var curPeriod string
	var curStart int64
	if err := r.DB.QueryRow(`SELECT quota_period, quota_period_start FROM users WHERE id=?`, id).Scan(&curPeriod, &curStart); err != nil {
		return err
	}
	if curPeriod != period || curStart == 0 {
		_, err := r.DB.Exec(
			`UPDATE users SET quota_bytes=?, quota_period=?, quota_warn_pct=?,
			                  quota_period_start=?, quota_used_bytes=0, quota_warned=0, quota_suspended=0
			 WHERE id=? AND deleted_at IS NULL`,
			bytes, period, warnPct, nowTS, id,
		)
		return err
	}
	_, err := r.DB.Exec(
		`UPDATE users SET quota_bytes=?, quota_warn_pct=? WHERE id=? AND deleted_at IS NULL`,
		bytes, warnPct, id,
	)
	return err
}

// SetSuspended toggles the quota_suspended flag.
func (r UserRepo) SetSuspended(id int64, suspended bool) error {
	_, err := r.DB.Exec(
		`UPDATE users SET quota_suspended=? WHERE id=? AND deleted_at IS NULL`,
		suspended, id,
	)
	return err
}

// ResetQuota zeroes the quota usage counters and advances period_start.
func (r UserRepo) ResetQuota(id int64, nowTS int64) error {
	_, err := r.DB.Exec(
		`UPDATE users SET quota_used_bytes=0, quota_warned=0, quota_suspended=0, quota_period_start=?
		 WHERE id=? AND deleted_at IS NULL`,
		nowTS, id,
	)
	return err
}

// ActiveCount returns the number of enabled non-deleted users.
func (r UserRepo) ActiveCount() (int, error) {
	var n int
	err := r.DB.QueryRow(
		`SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND enabled=1`,
	).Scan(&n)
	return n, err
}

// Create inserts a new user and returns its ID.
// Returns an error if the label already exists among non-deleted users or the limit is reached.
func (r UserRepo) Create(label, secretHex string) (int64, error) {
	n, err := r.ActiveCount()
	if err != nil {
		return 0, err
	}
	if n >= maxActiveUsers {
		return 0, fmt.Errorf("maximum %d active users reached", maxActiveUsers)
	}
	var exists int
	r.DB.QueryRow(
		`SELECT COUNT(*) FROM users WHERE label=? AND deleted_at IS NULL`, label,
	).Scan(&exists)
	if exists > 0 {
		return 0, fmt.Errorf("label %q already exists", label)
	}
	res, err := r.DB.Exec(
		`INSERT INTO users(label, secret_hex) VALUES(?,?)`, label, secretHex,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetEnabled enables or disables a user.
func (r UserRepo) SetEnabled(id int64, enabled bool) error {
	_, err := r.DB.Exec(`UPDATE users SET enabled=? WHERE id=? AND deleted_at IS NULL`, enabled, id)
	return err
}

// Delete soft-deletes a user.
func (r UserRepo) Delete(id int64) error {
	_, err := r.DB.Exec(
		`UPDATE users SET deleted_at=datetime('now') WHERE id=? AND deleted_at IS NULL`, id,
	)
	return err
}

// Restore clears deleted_at after a failed apply path.
func (r UserRepo) Restore(id int64) error {
	_, err := r.DB.Exec(
		`UPDATE users SET deleted_at=NULL WHERE id=?`,
		id,
	)
	return err
}

// UpdateSecret replaces the secret_hex and sets rotated_at.
func (r UserRepo) UpdateSecret(id int64, secretHex string) error {
	_, err := r.DB.Exec(
		`UPDATE users SET secret_hex=?, rotated_at=datetime('now') WHERE id=? AND deleted_at IS NULL`,
		secretHex, id,
	)
	return err
}

// --- HTTP handlers ---

func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	repo := UserRepo{DB: s.DB}
	users, err := repo.List()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	tok, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	userListPage(w, users, tok, "")
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	if err := secrets.ValidateUserLabel(label); err != nil {
		repo := UserRepo{DB: s.DB}
		users, _ := repo.List()
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		userListPage(w, users, tok, "Invalid label: "+err.Error())
		return
	}

	secret, err := secrets.GenerateMTProtoSecret()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	repo := UserRepo{DB: s.DB}
	id, err := repo.Create(label, secret.Hex())
	if err != nil {
		users, _ := repo.List()
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		userListPage(w, users, tok, err.Error())
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "user.create", label, fmt.Sprintf("id=%d", id), clientIP(r)) //nolint:errcheck
	if err := s.reloadTeleproxy(); err != nil {
		_ = repo.Delete(id)
		http.Error(w, "failed to apply teleproxy config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	userCreatedPage(w, label, secret.Hex())
}

// sessionAdminID returns the admin ID from the current session, or 0.
func (s *Server) sessionAdminID(r *http.Request) int64 {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return 0
	}
	var adminID int64
	s.DB.QueryRow(`SELECT admin_id FROM sessions WHERE id=?`, cookie.Value).Scan(&adminID) //nolint:errcheck
	return adminID
}

// ErrNotFound is returned when a user row is not found.
var ErrNotFound = errors.New("not found")
