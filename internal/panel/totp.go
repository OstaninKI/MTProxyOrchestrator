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
	totpVerifyPage(w, totpVerifyData{
		CSRFField: CSRFField(),
		CSRFToken: tok,
		PanelPath: s.PanelPath,
	})
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
			PanelPath: s.PanelPath,
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
		PanelPath: s.PanelPath,
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
		PanelPath:   s.PanelPath,
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
			PanelPath:   s.PanelPath,
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
		PanelPath:     s.PanelPath,
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
			PanelPath: s.PanelPath,
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
		PanelPath: s.PanelPath,
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
			PanelPath: s.PanelPath,
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
		PanelPath:     s.PanelPath,
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
	PanelPath string
}

type totpSettingsData struct {
	CSRFField  string
	CSRFToken  string
	Enabled    bool
	Success    string
	Error      string
	CurrentNav string
	PanelPath  string
}

type totpEnrollData struct {
	CSRFField   string
	CSRFToken   string
	Secret      string
	OTPAuthURL  string
	QRPNGBase64 string
	Error       string
	CurrentNav  string
	PanelPath   string
}

type totpRecoveryData struct {
	CSRFField     string
	CSRFToken     string
	RecoveryCodes []string
	Heading       string
	CurrentNav    string
	PanelPath     string
}

var totpVerifyTmpl = template.Must(template.New("totp_verify").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Two-factor verification</title>
<link rel="stylesheet" href="{{.PanelPath}}assets/panel.css">
</head>
<body class="login-page">
<div class="app login-app">
<main class="login-shell">
<div class="card login-card">
<p class="page-eyebrow">MTProto Orchestrator</p>
<h1>Two-Factor Verification</h1>
<p class="page-sub">Enter the code from your authenticator app or one of your recovery codes.</p>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" action="{{.PanelPath}}totp/verify">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Authenticator code or recovery code</label>
<input type="text" name="code" autocomplete="one-time-code" required autofocus>
<button type="submit">Verify</button>
</form>
<p class="panel-note">Lost your device? Enter one of your recovery codes.</p>
</div>
</main>
</div>
</body>
</html>`))

func totpVerifyPage(w io.Writer, data totpVerifyData) {
	totpVerifyTmpl.Execute(w, data) //nolint:errcheck
}

var totpSettingsTmpl = layoutTemplate("totp_settings", `{{define "page_title"}}Two-factor authentication{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Settings</h1>
    <p class="page-sub">Endpoint, password and system configuration.</p>
  </div>
</section>
<section class="page-stack">
<nav class="seg" aria-label="Two-factor tabs">
  <a class="seg-item" href="{{.PanelPath}}settings/proxy">Endpoint &amp; Proxy</a>
  <a class="seg-item" href="{{.PanelPath}}settings/admin-password">Admin password</a>
  <a class="seg-item" href="{{.PanelPath}}settings/system">System</a>
  <a class="seg-item active" href="{{.PanelPath}}settings/totp">Two-factor</a>
</nav>
{{if .Success}}<p class="success">{{.Success}}</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<section class="grid-12">
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon">{{icon "Shield" 15}}</span><span class="stat-label">Protection status</span></div><strong class="stat-value">{{if .Enabled}}Enabled{{else}}Disabled{{end}}</strong><span class="stat-hint">{{if .Enabled}}Second factor required{{else}}Password only{{end}}</span></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon">{{icon "Lock" 15}}</span><span class="stat-label">Login gate</span></div><strong class="stat-value">TOTP</strong><span class="stat-hint">Authenticator or recovery code</span></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon" data-tone="success">{{icon "Key" 15}}</span><span class="stat-label">Recovery mode</span></div><strong class="stat-value">{{if .Enabled}}Available{{else}}Pending{{end}}</strong><span class="stat-hint">{{if .Enabled}}Codes can be rotated{{else}}Generated on enable{{end}}</span></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon" data-tone="warn">{{icon "Activity" 15}}</span><span class="stat-label">Operator action</span></div><strong class="stat-value">{{if .Enabled}}Maintain{{else}}Enable{{end}}</strong><span class="stat-hint">{{if .Enabled}}Disable or regenerate{{else}}Start setup{{end}}</span></div>
  </article>
</section>

{{if .Enabled}}
<div class="grid-12">
<div class="col-7">
<div class="card">
<div class="card-head"><div class="col card-title-stack"><h3>Disable two-factor</h3><span class="sub">Require the current authenticator code or a valid recovery code.</span></div></div>
<div class="card-body"><form method="post" action="{{.PanelPath}}settings/totp/disable" class="stack-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<div class="field"><label class="label">Current authenticator or recovery code</label><input class="input input--mono" type="text" name="code" autocomplete="one-time-code" required><span class="help">Disabling clears the stored secret and recovery code set.</span></div>
<button class="btn danger" type="submit">Disable</button>
</form></div>
</div>

<div class="card">
<div class="card-head"><div class="col card-title-stack"><h3>Rotate recovery codes</h3><span class="sub">Generate a fresh one-time recovery set.</span></div></div>
<div class="card-body"><form method="post" action="{{.PanelPath}}settings/totp/regenerate" class="stack-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<div class="field"><label class="label">Current authenticator code</label><input class="input input--mono" type="text" name="code" autocomplete="one-time-code" required><span class="help">Previously generated recovery codes are invalidated immediately.</span></div>
<button class="btn" data-variant="primary" type="submit">Regenerate codes</button>
</form></div>
</div>
</div>
<aside class="col-5 card">
<div class="card-head"><h3>Setup notes</h3></div>
<div class="card-body col col-panel">
  <div class="totp-note-row"><span class="badge ok">Active</span><span class="col totp-note-copy"><strong class="totp-note-title">Second factor enforced</strong><span class="help">Password login now requires a current authenticator or recovery code.</span></span></div>
  <div class="totp-note-row"><span class="badge warn">Recovery</span><span class="col totp-note-copy"><strong class="totp-note-title">One-time only</strong><span class="help">Each recovery code can be consumed once and should be stored offline.</span></span></div>
  <div class="totp-note-row"><span class="badge">Audit</span><span class="col totp-note-copy"><strong class="totp-note-title">Administrative action</strong><span class="help">Enable, disable, and regeneration events are recorded.</span></span></div>
</div>
</aside>
</div>
{{else}}
<div class="grid-12">
<div class="col-7 card">
<div class="card-head"><div class="col card-title-stack"><h3>Enable two-factor</h3><span class="sub">Generate a TOTP secret, scan it, and confirm with a current code.</span></div></div>
<div class="card-body"><form method="post" action="{{.PanelPath}}settings/totp/begin" class="stack-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<button class="btn" data-variant="primary" type="submit">{{icon "Shield" 13}} Enable two-factor</button>
</form></div>
</div>
<aside class="col-5 card">
<div class="card-head"><h3>Setup notes</h3></div>
<div class="card-body col col-panel">
  <div class="totp-note-row"><span class="badge">App</span><span class="col totp-note-copy"><strong class="totp-note-title">Authenticator required</strong><span class="help">Any TOTP-compatible authenticator app can scan the QR code.</span></span></div>
  <div class="totp-note-row"><span class="badge ok">Recovery</span><span class="col totp-note-copy"><strong class="totp-note-title">Codes generated on enable</strong><span class="help">Recovery codes are shown once after confirmation.</span></span></div>
  <div class="totp-note-row"><span class="badge warn">Scope</span><span class="col totp-note-copy"><strong class="totp-note-title">Admin logins only</strong><span class="help">This does not change MTProto user secrets.</span></span></div>
</div>
</aside>
</div>
{{end}}
</section>
{{end}}
{{template "base" .}}`, nil)

func totpSettingsPage(w io.Writer, data totpSettingsData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "settings"
	}
	totpSettingsTmpl.Execute(w, data) //nolint:errcheck
}

var totpEnrollTmpl = layoutTemplate("totp_enroll", `{{define "page_title"}}Enable two-factor{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Enable Two-Factor</h1>
    <p class="page-sub">Scan the QR code with your authenticator app or enter the secret manually.</p>
  </div>
  <div class="actions"><a class="btn" data-variant="ghost" href="{{.PanelPath}}settings/totp">Back to two-factor</a></div>
</section>
<section class="page-stack">
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<section class="grid-12">
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon">{{icon "Shield" 15}}</span><span class="stat-label">Factor type</span></div><strong class="stat-value">TOTP</strong><span class="stat-hint">6-digit authenticator codes</span></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon">{{icon "Cert" 15}}</span><span class="stat-label">Issuer</span></div><strong class="stat-value">tgproxy-panel</strong><span class="stat-hint">Authenticator account label</span></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon" data-tone="success">{{icon "Check" 15}}</span><span class="stat-label">Confirmation</span></div><strong class="stat-value">One code</strong><span class="stat-hint">After scanning</span></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon" data-tone="warn">{{icon "Key" 15}}</span><span class="stat-label">Recovery</span></div><strong class="stat-value">Issued next</strong><span class="stat-hint">After confirmation</span></div>
  </article>
</section>
<div class="stack-split">
<div class="card totp-qr-card">
<div class="card-body">
<h2>Authenticator Setup</h2>
<p class="panel-note">Scan the QR code if possible. If the device cannot scan, copy the secret or the full otpauth URL manually.</p>
</div>
<div class="totp-qr-frame">
  <img src="data:image/png;base64,{{.QRPNGBase64}}" alt="TOTP QR code">
</div>
<div class="summary-list totp-fields">
  <div class="summary-row">
    <span class="badge ok">Secret</span>
    <span class="summary-copy"><strong class="mono-chip">{{.Secret}}</strong><span>Manual shared secret for TOTP-compatible apps</span></span>
  </div>
  <div class="summary-row">
    <span class="badge">URI</span>
    <span class="summary-copy"><strong class="mono-chip">{{.OTPAuthURL}}</strong><span>Full otpauth enrollment URL if QR scanning is unavailable</span></span>
  </div>
</div>
</div>
<div class="page-stack">
<div class="card form-panel">
<div class="card-body">
<h2>Confirm enrollment</h2>
<p class="panel-note">Enter the current 6-digit code from the authenticator app to finish enabling two-factor and generate recovery codes.</p>
</div>
<form method="post" action="{{.PanelPath}}settings/totp/confirm" class="stack-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Enter the 6-digit code from your app</label>
<input type="text" name="code" autocomplete="one-time-code" required autofocus>
<button type="submit">Confirm and enable</button>
</form>
</div>
<aside class="card side-panel">
<h2>Enrollment Notes</h2>
<div class="summary-list">
  <div class="summary-row">
    <span class="badge">Time</span>
    <span class="summary-copy"><strong>30-second window</strong><span>Authenticator codes rotate quickly, so enter the current value without delay.</span></span>
  </div>
  <div class="summary-row">
    <span class="badge warn">Storage</span>
    <span class="summary-copy"><strong>Write recovery codes down</strong><span>They are only revealed after confirmation and each can be used once.</span></span>
  </div>
</div>
</aside>
</div>
</div>
</section>
{{end}}
{{template "base" .}}`, nil)

func totpEnrollPage(w io.Writer, data totpEnrollData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "settings"
	}
	totpEnrollTmpl.Execute(w, data) //nolint:errcheck
}

var totpRecoveryTmpl = layoutTemplate("totp_recovery", `{{define "page_title"}}Recovery codes{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">{{.Heading}}</h1>
    <p class="page-sub">These codes can each be used once if the authenticator device is unavailable.</p>
  </div>
  <div class="actions"><a class="btn" data-variant="ghost" href="{{.PanelPath}}settings/totp">Back to two-factor</a></div>
</section>
<section class="page-stack">
<section class="grid-12">
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon">{{icon "Key" 15}}</span><span class="stat-label">Recovery codes</span></div><strong class="stat-value mono">{{len .RecoveryCodes}}</strong><span class="stat-hint">Single use each</span></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon" data-tone="warn">{{icon "Bell" 15}}</span><span class="stat-label">Visibility</span></div><strong class="stat-value">One time</strong><span class="stat-hint">Shown only now</span></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon" data-tone="success">{{icon "Download" 15}}</span><span class="stat-label">Storage</span></div><strong class="stat-value">Offline</strong><span class="stat-hint">Keep protected copy</span></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon">{{icon "Logout" 15}}</span><span class="stat-label">Fallback</span></div><strong class="stat-value">Login recovery</strong><span class="stat-hint">When device is unavailable</span></div>
  </article>
</section>
<div class="stack-split">
<div class="card">
  <div class="card-body">
    <h2>Recovery codes</h2>
    <p class="panel-note">Store this set now. The panel will not show the same recovery codes again after you leave this page.</p>
  </div>
  <div class="warn-box"><strong>Save these codes now.</strong> Each can be used once if you lose your authenticator. They will not be shown again.</div>
  <ul class="codes">
  {{range .RecoveryCodes}}<li>{{.}}</li>{{end}}
  </ul>
</div>
<aside class="card side-panel">
<h2>Handling Notes</h2>
<div class="summary-list">
  <div class="summary-row">
    <span class="badge warn">One-time</span>
    <span class="summary-copy"><strong>Single use only</strong><span>Any recovery code is consumed on first successful login and cannot be reused.</span></span>
  </div>
  <div class="summary-row">
    <span class="badge">Rotation</span>
    <span class="summary-copy"><strong>Regenerate from settings</strong><span>A fresh set replaces the current one immediately.</span></span>
  </div>
</div>
</aside>
</div>
</section>
{{end}}
{{template "base" .}}`, nil)

func totpRecoveryCodesPage(w io.Writer, data totpRecoveryData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "settings"
	}
	totpRecoveryTmpl.Execute(w, data) //nolint:errcheck
}
