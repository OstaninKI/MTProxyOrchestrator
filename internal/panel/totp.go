package panel

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
	totppkg "github.com/mtproto-orchestrator/mtproto-orchestrator/internal/totp"
	qrcode "github.com/skip2/go-qrcode"
)

// pendingTOTPCookieName carries the random ID of a half-finished login that
// still needs a second factor. Issued only after a correct password.
const pendingTOTPCookieName = "pending_totp"

// pendingTOTPTTL bounds how long an admin has between password and code.
const pendingTOTPTTL = 5 * time.Minute

// pendingTOTP holds the partial login state until the second factor is
// presented. Stored in-memory: 2FA tokens are short-lived and a panel restart
// invalidates them, which is acceptable.
type pendingTOTP struct {
	adminID   int64
	login     string
	expiresAt time.Time
}

type pendingTOTPStore struct {
	mu      sync.Mutex
	entries map[string]pendingTOTP
}

func newPendingTOTPStore() *pendingTOTPStore {
	return &pendingTOTPStore{entries: make(map[string]pendingTOTP)}
}

func (p *pendingTOTPStore) put(adminID int64, login string) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	id := hex.EncodeToString(buf)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gcLocked()
	p.entries[id] = pendingTOTP{
		adminID:   adminID,
		login:     login,
		expiresAt: time.Now().Add(pendingTOTPTTL),
	}
	return id, nil
}

func (p *pendingTOTPStore) get(id string) (pendingTOTP, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gcLocked()
	v, ok := p.entries[id]
	if !ok {
		return pendingTOTP{}, false
	}
	if time.Now().After(v.expiresAt) {
		delete(p.entries, id)
		return pendingTOTP{}, false
	}
	return v, true
}

func (p *pendingTOTPStore) drop(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.entries, id)
}

func (p *pendingTOTPStore) gcLocked() {
	now := time.Now()
	for k, v := range p.entries {
		if now.After(v.expiresAt) {
			delete(p.entries, k)
		}
	}
}

func (s *Server) pendingStore() *pendingTOTPStore {
	s.totpMu.Lock()
	defer s.totpMu.Unlock()
	if s.totpPending == nil {
		s.totpPending = newPendingTOTPStore()
	}
	return s.totpPending
}

// adminTOTP holds the admin row's 2FA columns.
type adminTOTP struct {
	enabled bool
	secret  string
}

func (s *Server) loadAdminTOTP(adminID int64) (adminTOTP, error) {
	var a adminTOTP
	var enabled int
	err := s.DB.QueryRow(`SELECT totp_enabled, totp_secret FROM admin WHERE id=?`, adminID).Scan(&enabled, &a.secret)
	if err != nil {
		return a, err
	}
	a.enabled = enabled == 1
	return a, nil
}

// issuePendingTOTP issues a short-lived cookie identifying a pending TOTP step.
func (s *Server) issuePendingTOTP(w http.ResponseWriter, adminID int64, login string) error {
	id, err := s.pendingStore().put(adminID, login)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     pendingTOTPCookieName,
		Value:    id,
		Path:     s.PanelPath,
		MaxAge:   int(pendingTOTPTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func (s *Server) clearPendingTOTP(w http.ResponseWriter, id string) {
	if id != "" {
		s.pendingStore().drop(id)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     pendingTOTPCookieName,
		Value:    "",
		Path:     s.PanelPath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.Secure,
	})
}

// finalizeLogin creates the session and audit row that the password handler
// would normally produce. Called from both the no-2FA path and after TOTP
// verification.
func (s *Server) finalizeLogin(w http.ResponseWriter, r *http.Request, adminID int64, loginName string) error {
	ip := clientIP(r)
	s.RateLimiter.RecordSuccess(ip)
	audit.Log(s.DB, adminID, "login_success", loginName, "", ip) //nolint:errcheck

	sessionID, err := NewSessionID()
	if err != nil {
		return err
	}
	exp := SessionExpiry()
	if _, err := s.DB.Exec(
		`INSERT INTO sessions(id, admin_id, expires_at, last_seen_at, ip) VALUES(?,?,?,?,?)`,
		sessionID, adminID, exp.UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), ip,
	); err != nil {
		return err
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
	return nil
}

// handleTOTPVerifyForm renders the TOTP prompt during login. Reachable only
// when a pending_totp cookie is set.
func (s *Server) handleTOTPVerifyForm(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(pendingTOTPCookieName)
	if err != nil || cookie.Value == "" {
		http.Redirect(w, r, strings.TrimSuffix(s.PanelPath, "/")+"/login", http.StatusSeeOther)
		return
	}
	if _, ok := s.pendingStore().get(cookie.Value); !ok {
		s.clearPendingTOTP(w, cookie.Value)
		http.Redirect(w, r, strings.TrimSuffix(s.PanelPath, "/")+"/login", http.StatusSeeOther)
		return
	}
	tok, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	totpVerifyPage(w, totpVerifyData{CSRFField: CSRFField(), CSRFToken: tok})
}

// handleTOTPVerifySubmit consumes a valid TOTP or recovery code and finalises
// the login. Failures count toward the same per-IP login rate limit.
func (s *Server) handleTOTPVerifySubmit(w http.ResponseWriter, r *http.Request) {
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
	cookie, err := r.Cookie(pendingTOTPCookieName)
	if err != nil || cookie.Value == "" {
		http.Redirect(w, r, strings.TrimSuffix(s.PanelPath, "/")+"/login", http.StatusSeeOther)
		return
	}
	pending, ok := s.pendingStore().get(cookie.Value)
	if !ok {
		s.clearPendingTOTP(w, cookie.Value)
		http.Redirect(w, r, strings.TrimSuffix(s.PanelPath, "/")+"/login", http.StatusSeeOther)
		return
	}

	code := strings.TrimSpace(r.FormValue("code"))
	a, err := s.loadAdminTOTP(pending.adminID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	verified := false
	usedRecovery := false
	if a.enabled && totppkg.Validate(a.secret, code) {
		verified = true
	} else if a.enabled {
		ok, _ := totppkg.ConsumeRecoveryCode(r.Context(), s.DB, pending.adminID, code)
		if ok {
			verified = true
			usedRecovery = true
		}
	}

	if !verified {
		s.RateLimiter.RecordFailure(ip)
		audit.Log(s.DB, pending.adminID, "totp_failed", pending.login, "", ip) //nolint:errcheck
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		totpVerifyPage(w, totpVerifyData{
			CSRFField: CSRFField(),
			CSRFToken: tok,
			Error:     "Invalid code",
		})
		return
	}

	s.clearPendingTOTP(w, cookie.Value)
	if err := s.finalizeLogin(w, r, pending.adminID, pending.login); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if usedRecovery {
		audit.Log(s.DB, pending.adminID, "totp_recovery_used", pending.login, "", ip) //nolint:errcheck
	}
	http.Redirect(w, r, s.PanelPath, http.StatusSeeOther)
}

// --- settings page (enable / disable / regenerate recovery codes) ---

func (s *Server) handleSettingsTOTPGet(w http.ResponseWriter, r *http.Request) {
	adminID := s.sessionAdminID(r)
	a, err := s.loadAdminTOTP(adminID)
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
	totpSettingsPage(w, totpSettingsData{
		CSRFField: CSRFField(),
		CSRFToken: tok,
		Enabled:   a.enabled,
	})
}

// handleSettingsTOTPBegin generates a fresh secret and shows the QR + setup
// form. The secret is held in the session-scoped pending_totp_setup cookie
// (in-memory store) until the user confirms with a valid code.
func (s *Server) handleSettingsTOTPBegin(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	adminID := s.sessionAdminID(r)
	a, err := s.loadAdminTOTP(adminID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if a.enabled {
		http.Redirect(w, r, s.PanelPath+"settings/totp", http.StatusSeeOther)
		return
	}

	var login string
	if err := s.DB.QueryRow(`SELECT login FROM admin WHERE id=?`, adminID).Scan(&login); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	secret, otpURL, err := totppkg.GenerateSecret(login)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Persist the candidate secret immediately but keep totp_enabled=0.
	// On confirm we just flip the flag and store recovery codes.
	if _, err := s.DB.Exec(`UPDATE admin SET totp_secret=?, totp_enabled=0, totp_recovery_codes='' WHERE id=?`, secret, adminID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	qrPNG, err := renderQRPNG(otpURL)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	totpEnrollPage(w, totpEnrollData{
		CSRFField:   CSRFField(),
		CSRFToken:   tok,
		Secret:      secret,
		OTPAuthURL:  otpURL,
		QRPNGBase64: qrPNG,
	})
}

func (s *Server) handleSettingsTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	adminID := s.sessionAdminID(r)
	a, err := s.loadAdminTOTP(adminID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if a.enabled || a.secret == "" {
		http.Redirect(w, r, s.PanelPath+"settings/totp", http.StatusSeeOther)
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if !totppkg.Validate(a.secret, code) {
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		login, _ := s.adminLogin(adminID)
		_, otpURL, _ := keyURL(a.secret, login)
		qrPNG, _ := renderQRPNG(otpURL)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		totpEnrollPage(w, totpEnrollData{
			CSRFField:   CSRFField(),
			CSRFToken:   tok,
			Secret:      a.secret,
			OTPAuthURL:  otpURL,
			QRPNGBase64: qrPNG,
			Error:       "Invalid code",
		})
		return
	}

	plain, hashes, err := totppkg.GenerateRecoveryCodes(8)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	encoded, err := totppkg.EncodeRecoveryHashes(hashes)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := s.DB.Exec(`UPDATE admin SET totp_enabled=1, totp_recovery_codes=? WHERE id=?`, encoded, adminID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	audit.Log(s.DB, adminID, "totp_enabled", "", "", clientIP(r)) //nolint:errcheck
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	totpRecoveryCodesPage(w, totpRecoveryData{
		CSRFField:     CSRFField(),
		CSRFToken:     tok,
		RecoveryCodes: plain,
		Heading:       "Two-factor authentication enabled",
	})
}

func (s *Server) handleSettingsTOTPDisable(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	adminID := s.sessionAdminID(r)
	a, err := s.loadAdminTOTP(adminID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !a.enabled {
		http.Redirect(w, r, s.PanelPath+"settings/totp", http.StatusSeeOther)
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	verified := totppkg.Validate(a.secret, code)
	if !verified {
		ok, _ := totppkg.ConsumeRecoveryCode(r.Context(), s.DB, adminID, code)
		verified = ok
	}
	if !verified {
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		totpSettingsPage(w, totpSettingsData{
			CSRFField: CSRFField(),
			CSRFToken: tok,
			Enabled:   true,
			Error:     "Invalid code; cannot disable two-factor.",
		})
		return
	}
	if _, err := s.DB.Exec(`UPDATE admin SET totp_enabled=0, totp_secret='', totp_recovery_codes='' WHERE id=?`, adminID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	audit.Log(s.DB, adminID, "totp_disabled", "", "", clientIP(r)) //nolint:errcheck
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	totpSettingsPage(w, totpSettingsData{
		CSRFField: CSRFField(),
		CSRFToken: tok,
		Enabled:   false,
		Success:   "Two-factor authentication disabled.",
	})
}

func (s *Server) handleSettingsTOTPRegenerate(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	adminID := s.sessionAdminID(r)
	a, err := s.loadAdminTOTP(adminID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !a.enabled {
		http.Redirect(w, r, s.PanelPath+"settings/totp", http.StatusSeeOther)
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if !totppkg.Validate(a.secret, code) {
		tok, _ := NewCSRFToken()
		SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		totpSettingsPage(w, totpSettingsData{
			CSRFField: CSRFField(),
			CSRFToken: tok,
			Enabled:   true,
			Error:     "Invalid code; recovery codes not regenerated.",
		})
		return
	}
	plain, hashes, err := totppkg.GenerateRecoveryCodes(8)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	encoded, err := totppkg.EncodeRecoveryHashes(hashes)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := s.DB.Exec(`UPDATE admin SET totp_recovery_codes=? WHERE id=?`, encoded, adminID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	audit.Log(s.DB, adminID, "totp_recovery_regenerated", "", "", clientIP(r)) //nolint:errcheck
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	totpRecoveryCodesPage(w, totpRecoveryData{
		CSRFField:     CSRFField(),
		CSRFToken:     tok,
		RecoveryCodes: plain,
		Heading:       "New recovery codes",
	})
}

func (s *Server) adminLogin(adminID int64) (string, error) {
	var login string
	err := s.DB.QueryRow(`SELECT login FROM admin WHERE id=?`, adminID).Scan(&login)
	return login, err
}

// keyURL rebuilds the otpauth URL from a stored secret without persisting a new
// secret — used when re-rendering the enroll page after a bad confirmation.
func keyURL(secret, account string) (string, string, error) {
	v := fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		totppkg.Issuer, account, secret, totppkg.Issuer)
	return secret, v, nil
}

func renderQRPNG(content string) (string, error) {
	q, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}
	img := q.Image(192)
	var buf strings.Builder
	enc := base64.NewEncoder(base64.StdEncoding, &b64Writer{w: &buf})
	if err := png.Encode(enc, img); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// b64Writer adapts a strings.Builder to io.Writer for the base64 encoder.
type b64Writer struct{ w *strings.Builder }

func (b *b64Writer) Write(p []byte) (int, error) { return b.w.Write(p) }

var _ io.Writer = (*b64Writer)(nil)

// --- templates ---

type totpVerifyData struct {
	CSRFField string
	CSRFToken string
	Error     string
}

type totpSettingsData struct {
	CSRFField string
	CSRFToken string
	Enabled   bool
	Success   string
	Error     string
}

type totpEnrollData struct {
	CSRFField   string
	CSRFToken   string
	Secret      string
	OTPAuthURL  string
	QRPNGBase64 string
	Error       string
}

type totpRecoveryData struct {
	CSRFField     string
	CSRFToken     string
	RecoveryCodes []string
	Heading       string
}

var totpVerifyTmpl = template.Must(template.New("totp_verify").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Two-factor verification</title>
<style>body{font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f5f5}
.card{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,.12);width:320px}
h1{margin:0 0 1rem;font-size:1.2rem}label{display:block;margin-bottom:.25rem;font-size:.875rem;color:#555}
input{width:100%;box-sizing:border-box;padding:.5rem;border:1px solid #ccc;border-radius:4px;margin-bottom:1rem;font-size:1rem}
button{width:100%;padding:.6rem;background:#2563eb;color:#fff;border:none;border-radius:4px;font-size:1rem;cursor:pointer}
.error{color:#dc2626;margin-bottom:1rem;font-size:.875rem}p.hint{font-size:.8rem;color:#555}</style>
</head><body><div class="card">
<h1>Two-factor verification</h1>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" action="totp/verify">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Authenticator code or recovery code</label>
<input type="text" name="code" autocomplete="one-time-code" required autofocus>
<button type="submit">Verify</button>
</form>
<p class="hint">Lost your device? Enter one of your recovery codes.</p>
</div></body></html>`))

func totpVerifyPage(w io.Writer, data totpVerifyData) {
	totpVerifyTmpl.Execute(w, data) //nolint:errcheck
}

var totpSettingsTmpl = template.Must(template.New("totp_settings").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Two-factor authentication</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1{margin-bottom:1rem}
form{max-width:480px;margin-bottom:1.5rem}label{display:block;margin-top:1rem;font-weight:bold}
input[type=text]{width:100%;padding:.5rem;margin-top:.25rem;border:1px solid #ccc;border-radius:4px;box-sizing:border-box}
button{margin-top:1rem;padding:.5rem 1rem;background:#2563eb;color:#fff;border:none;border-radius:4px;cursor:pointer}
.danger{background:#b91c1c}.success{color:#16a34a;margin-bottom:1rem}.error{color:#dc2626;margin-bottom:1rem}
.box{background:#fff;border:1px solid #e5e7eb;border-radius:6px;padding:1rem;margin-bottom:1rem}
a{color:#2563eb}</style>
</head><body>
<h1>Two-factor authentication</h1>
<p><a href="../dashboard">← Dashboard</a> &nbsp;|&nbsp; <a href="admin-password">Admin password</a></p>
{{if .Success}}<p class="success">{{.Success}}</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

{{if .Enabled}}
<div class="box"><strong>Status:</strong> enabled.</div>

<form method="post" action="totp/disable">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Disable two-factor — enter current code</label>
<input type="text" name="code" autocomplete="one-time-code" required>
<button class="danger" type="submit">Disable</button>
</form>

<form method="post" action="totp/regenerate">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Regenerate recovery codes — enter current code</label>
<input type="text" name="code" autocomplete="one-time-code" required>
<button type="submit">Regenerate codes</button>
</form>
{{else}}
<div class="box"><strong>Status:</strong> disabled.</div>
<form method="post" action="totp/begin">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<button type="submit">Enable two-factor</button>
</form>
{{end}}
</body></html>`))

func totpSettingsPage(w io.Writer, data totpSettingsData) {
	totpSettingsTmpl.Execute(w, data) //nolint:errcheck
}

var totpEnrollTmpl = template.Must(template.New("totp_enroll").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Enable two-factor</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1{margin-bottom:1rem}
form{max-width:480px}label{display:block;margin-top:1rem;font-weight:bold}
input[type=text]{width:100%;padding:.5rem;margin-top:.25rem;border:1px solid #ccc;border-radius:4px;box-sizing:border-box}
button{margin-top:1rem;padding:.5rem 1rem;background:#2563eb;color:#fff;border:none;border-radius:4px;cursor:pointer}
.error{color:#dc2626;margin-bottom:1rem}.mono{font-family:monospace;background:#f3f4f6;padding:.25rem .5rem;border-radius:4px}
.qr{margin:1rem 0}img{display:block}</style>
</head><body>
<h1>Enable two-factor</h1>
<p>Scan the QR code with your authenticator app, or enter the secret manually.</p>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<div class="qr"><img src="data:image/png;base64,{{.QRPNGBase64}}" alt="TOTP QR code"></div>
<p>Secret: <span class="mono">{{.Secret}}</span></p>
<p>otpauth URL: <span class="mono">{{.OTPAuthURL}}</span></p>
<form method="post" action="totp/confirm">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Enter the 6-digit code from your app</label>
<input type="text" name="code" autocomplete="one-time-code" required autofocus>
<button type="submit">Confirm and enable</button>
</form>
</body></html>`))

func totpEnrollPage(w io.Writer, data totpEnrollData) {
	totpEnrollTmpl.Execute(w, data) //nolint:errcheck
}

var totpRecoveryTmpl = template.Must(template.New("totp_recovery").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Recovery codes</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1{margin-bottom:1rem}
ul{list-style:none;padding:0;font-family:monospace;font-size:1.05rem;background:#f3f4f6;padding:1rem;border-radius:6px;max-width:320px}
li{margin:.25rem 0}.warn{background:#fffbeb;border:1px solid #fed7aa;padding:1rem;border-radius:6px;color:#92400e;max-width:480px;margin-bottom:1rem}
a{color:#2563eb}</style>
</head><body>
<h1>{{.Heading}}</h1>
<div class="warn"><strong>Save these codes now.</strong> Each can be used once if you lose your authenticator. They will not be shown again.</div>
<ul>
{{range .RecoveryCodes}}<li>{{.}}</li>{{end}}
</ul>
<p><a href="../totp">← Back to two-factor settings</a></p>
</body></html>`))

func totpRecoveryCodesPage(w io.Writer, data totpRecoveryData) {
	totpRecoveryTmpl.Execute(w, data) //nolint:errcheck
}
