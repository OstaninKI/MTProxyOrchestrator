package panel

import (
	"net/http"
	"strings"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
)

const sessionCookieName = "session_id"

// requireAuth is a middleware that redirects unauthenticated requests to login.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthenticated(r) {
			http.Redirect(w, r, strings.TrimSuffix(s.PanelPath, "/")+"/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	var expiresAt string
	err = s.DB.QueryRow(
		`SELECT expires_at FROM sessions WHERE id=?`, cookie.Value,
	).Scan(&expiresAt)
	if err != nil {
		return false
	}
	exp, err := time.Parse("2006-01-02 15:04:05", expiresAt)
	if err != nil {
		return false
	}
	return time.Now().Before(exp)
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	tok, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, tok, s.Secure)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	loginPage(w, tok, "")
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)

	if s.RateLimiter.IsBlocked(ip) {
		audit.Log(s.DB, 0, "login_rate_limited", "", "", ip) //nolint:errcheck
		http.Error(w, "too many failed attempts, try again later", http.StatusTooManyRequests)
		return
	}

	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	login := r.FormValue("login")
	password := r.FormValue("password")

	var adminID int64
	var hash string
	err := s.DB.QueryRow(`SELECT id, password_hash FROM admin WHERE login=?`, login).Scan(&adminID, &hash)
	if err != nil || !CheckPassword(hash, password) {
		s.RateLimiter.RecordFailure(ip)
		audit.Log(s.DB, 0, "login_failed", login, "", ip) //nolint:errcheck
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		loginPage(w, tok, "Invalid login or password")
		return
	}

	s.RateLimiter.RecordSuccess(ip)
	audit.Log(s.DB, adminID, "login_success", login, "", ip) //nolint:errcheck

	sessionID, err := NewSessionID()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	exp := SessionExpiry()
	_, err = s.DB.Exec(
		`INSERT INTO sessions(id, admin_id, expires_at, ip) VALUES(?,?,?,?)`,
		sessionID, adminID, exp.UTC().Format("2006-01-02 15:04:05"), ip,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     s.PanelPath,
		MaxAge:   int(sessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, s.PanelPath, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	ip := clientIP(r)
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		adminID := s.sessionAdminID(r)
		s.DB.Exec(`DELETE FROM sessions WHERE id=?`, cookie.Value) //nolint:errcheck
		audit.Log(s.DB, adminID, "logout", "", "", ip)             //nolint:errcheck
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     s.PanelPath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.Secure,
	})
	http.Redirect(w, r, strings.TrimSuffix(s.PanelPath, "/")+"/login", http.StatusSeeOther)
}

// clientIP returns the real client IP for rate limiting.
// X-Forwarded-For is trusted only when the direct connection comes from loopback
// (i.e. nginx reverse proxy on the same host). Direct connections from non-loopback
// addresses use RemoteAddr so an attacker cannot spoof their IP via the header.
func clientIP(r *http.Request) string {
	remoteHost := r.RemoteAddr
	if i := strings.LastIndex(remoteHost, ":"); i >= 0 {
		remoteHost = remoteHost[:i]
	}
	if isLoopback(remoteHost) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
		}
	}
	return remoteHost
}

func isLoopback(host string) bool {
	return host == "127.0.0.1" || host == "::1" || host == "[::1]"
}
