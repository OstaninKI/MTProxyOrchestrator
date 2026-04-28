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

// UserRow is one row from the users table.
type UserRow struct {
	ID        int64
	Label     string
	SecretHex string
	Enabled   bool
	CreatedAt time.Time
	RotatedAt *time.Time
	DeletedAt *time.Time
}

// UserRepo wraps DB operations for users.
type UserRepo struct {
	DB *db.DB
}

// List returns all non-deleted users ordered by creation time.
func (r UserRepo) List() ([]UserRow, error) {
	rows, err := r.DB.Query(
		`SELECT id, label, secret_hex, enabled, created_at, rotated_at
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
		if err := rows.Scan(&u.ID, &u.Label, &u.SecretHex, &u.Enabled, &created, &rotated); err != nil {
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
	SetCSRFCookie(w, tok, s.Secure)
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
		SetCSRFCookie(w, tok, s.Secure)
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
		SetCSRFCookie(w, tok, s.Secure)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		userListPage(w, users, tok, err.Error())
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "user.create", label, fmt.Sprintf("id=%d", id), clientIP(r)) //nolint:errcheck

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
