package panel

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
)

// Setting key constants for the settings table.
const (
	settingMaskHost            = "mask_host"
	settingTLSBackend          = "tls_backend"
	settingWildcardMask        = "wildcard_mask"
	settingMSSClamp            = "mss_clamp"
	settingRandomPadding       = "random_padding"
	settingJA4Log              = "ja4_log"
	settingMTProtoPort         = "mtproto_port"
	settingServerIP            = "server_ip"
	settingPanelPath           = "panel_path"
	settingLogLevel            = "log_level"
	settingRetentionMinuteDays = "retention_minutes_days"
	settingRetentionHourlyDays = "retention_hourly_days"
	settingCertRenewDays       = "cert_renew_days"
	settingBridgeMode          = "bridge_mode"
)

// loadAdminSessions loads active admin sessions from the DB.
func (s *Server) loadAdminSessions(r *http.Request) []sessionView {
	// Get current session ID from cookie.
	currentID := ""
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		currentID = cookie.Value
	}

	// Get admin ID.
	adminID := s.sessionAdminID(r)

	// Query active sessions.
	rows, err := s.DB.Query(`
		SELECT id, COALESCE(ip,''), COALESCE(last_seen_at, created_at)
		FROM sessions
		WHERE admin_id=? AND expires_at > datetime('now')
		ORDER BY last_seen_at DESC
	`, adminID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var sessions []sessionView
	for rows.Next() {
		var id, ip, tsStr string
		if err := rows.Scan(&id, &ip, &tsStr); err != nil {
			continue
		}

		// Parse timestamp and reformat.
		lastSeen := tsStr
		if t, err := parseSessionTime(tsStr); err == nil {
			lastSeen = t.UTC().Format("2006-01-02 15:04 UTC")
		}

		// Handle empty IP.
		if ip == "" {
			ip = "—"
		}

		sessions = append(sessions, sessionView{
			ID:       id,
			IP:       ip,
			LastSeen: lastSeen,
			Current:  (id == currentID),
		})
	}

	return sessions
}

// handleSettingsProxyGet renders the proxy settings form.
func (s *Server) handleSettingsProxyGet(w http.ResponseWriter, r *http.Request) {
	tok, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	proxySettingsPage(w, proxySettingsData{
		CSRFField:     CSRFField(),
		CSRFToken:     tok,
		MaskHost:      s.bridgeMaskHost(),
		TLSBackend:    s.bridgeTLSBackend(),
		WildcardMask:  s.bridgeWildcardMask(),
		MSSClamp:      s.bridgeMSSClamp(),
		RandomPadding: s.bridgeRandomPadding(),
		JA4Log:        s.bridgeJA4Log(),
		MTProtoPort:   s.bridgeMTProtoPort(),
		ServerAddr:    s.settingsConfig().ServerIP,
		PanelPath:     s.PanelPath,
	})
}

// handleSettingsProxyPost processes proxy settings changes.
// Form fields: mask_host (required), mtproto_port (required, 1-65535).
func (s *Server) handleSettingsProxyPost(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	maskHost := strings.TrimSpace(r.FormValue("mask_host"))
	tlsBackend := strings.TrimSpace(r.FormValue("tls_backend"))
	wildcardMask := strings.TrimSpace(r.FormValue("wildcard_mask"))
	mssClamp := r.FormValue("mss_clamp") == "1"
	randomPadding := r.FormValue("random_padding") == "1"
	ja4Log := r.FormValue("ja4_log") == "1"
	portStr := strings.TrimSpace(r.FormValue("mtproto_port"))
	serverAddr := strings.TrimSpace(r.FormValue("server_addr"))

	// Validate mask_host.
	if maskHost == "" {
		s.renderProxySettingsError(w, r, s.Secure, s.PanelPath, maskHost, tlsBackend, wildcardMask, mssClamp, randomPadding, ja4Log, s.bridgeMTProtoPort(), serverAddr, "mask host is required")
		return
	}
	if !isValidMaskHost(maskHost) {
		s.renderProxySettingsError(w, r, s.Secure, s.PanelPath, maskHost, tlsBackend, wildcardMask, mssClamp, randomPadding, ja4Log, s.bridgeMTProtoPort(), serverAddr, "mask host must be a valid hostname")
		return
	}
	if tlsBackend != "" && !isValidTLSBackend(tlsBackend) {
		s.renderProxySettingsError(w, r, s.Secure, s.PanelPath, maskHost, tlsBackend, wildcardMask, mssClamp, randomPadding, ja4Log, s.bridgeMTProtoPort(), serverAddr, "TLS backend must be host:port or unix:/path")
		return
	}
	if wildcardMask != "" && !isValidWildcardMask(wildcardMask) {
		s.renderProxySettingsError(w, r, s.Secure, s.PanelPath, maskHost, tlsBackend, wildcardMask, mssClamp, randomPadding, ja4Log, s.bridgeMTProtoPort(), serverAddr, "wildcard mask must look like *.example.com")
		return
	}
	if wildcardMask != "" && tlsBackend == "" {
		s.renderProxySettingsError(w, r, s.Secure, s.PanelPath, maskHost, tlsBackend, wildcardMask, mssClamp, randomPadding, ja4Log, s.bridgeMTProtoPort(), serverAddr, "wildcard mask requires an explicit TLS backend")
		return
	}

	// Validate port.
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		s.renderProxySettingsError(w, r, s.Secure, s.PanelPath, maskHost, tlsBackend, wildcardMask, mssClamp, randomPadding, ja4Log, s.bridgeMTProtoPort(), serverAddr, "port must be between 1 and 65535")
		return
	}

	// Save to DB.
	if err := s.DB.SetSetting(settingMaskHost, maskHost); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	for key, value := range map[string]string{
		settingTLSBackend:    tlsBackend,
		settingWildcardMask:  wildcardMask,
		settingMSSClamp:      boolSetting(mssClamp),
		settingRandomPadding: boolSetting(randomPadding),
		settingJA4Log:        boolSetting(ja4Log),
	} {
		if err := s.DB.SetSetting(key, value); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if err := s.DB.SetSetting(settingMTProtoPort, portStr); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.DB.SetSetting(settingServerIP, serverAddr); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Update runtime config before reload so saved values are rendered immediately.
	if s.BridgeCfg == nil {
		s.BridgeCfg = &BridgeConfig{}
	}
	s.BridgeCfg.MaskHost = maskHost
	s.BridgeCfg.TLSBackend = tlsBackend
	s.BridgeCfg.WildcardMask = wildcardMask
	s.BridgeCfg.MSSClamp = mssClamp
	s.BridgeCfg.RandomPadding = randomPadding
	s.BridgeCfg.JA4Log = ja4Log
	s.BridgeCfg.MTProtoPort = port
	if s.SettingsCfg != nil {
		s.SettingsCfg.ServerIP = serverAddr
	}

	// Reload Teleproxy.
	if err := s.reloadTeleproxy(); err != nil {
		s.renderProxySettingsError(w, r, s.Secure, s.PanelPath, maskHost, tlsBackend, wildcardMask, mssClamp, randomPadding, ja4Log, port, serverAddr, "failed to reload teleproxy")
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "settings.proxy", "", "", clientIP(r)) //nolint:errcheck

	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	proxySettingsPage(w, proxySettingsData{
		CSRFField:     CSRFField(),
		CSRFToken:     tok,
		MaskHost:      maskHost,
		TLSBackend:    tlsBackend,
		WildcardMask:  wildcardMask,
		MSSClamp:      mssClamp,
		RandomPadding: randomPadding,
		JA4Log:        ja4Log,
		MTProtoPort:   port,
		ServerAddr:    serverAddr,
		Success:       "Proxy settings saved.",
		PanelPath:     s.PanelPath,
	})
}

func boolSetting(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// handleSettingsSessionRevoke revokes a session by its ID.
func (s *Server) handleSettingsSessionRevoke(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Get current session ID from cookie.
	currentID := ""
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		currentID = cookie.Value
	}

	// Refuse to revoke the current session.
	if id == currentID {
		http.Redirect(w, r, s.PanelPath+"settings/admin-password", http.StatusSeeOther)
		return
	}

	adminID := s.sessionAdminID(r)

	// Delete the session from DB.
	_, _ = s.DB.Exec("DELETE FROM sessions WHERE id=? AND admin_id=?", id, adminID)

	// Audit log.
	audit.Log(s.DB, adminID, "session.revoke", "", id, clientIP(r)) //nolint:errcheck

	http.Redirect(w, r, s.PanelPath+"settings/admin-password", http.StatusSeeOther)
}

// handleSettingsAdminPasswordGet renders the admin password change form.
func (s *Server) handleSettingsAdminPasswordGet(w http.ResponseWriter, r *http.Request) {
	tok, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	adminPasswordPage(w, adminPasswordData{
		CSRFField: CSRFField(),
		CSRFToken: tok,
		PanelPath: s.PanelPath,
		Sessions:  s.loadAdminSessions(r),
	})
}

// handleSettingsAdminPasswordPost processes admin password changes.
// Form fields: current_password, new_password, confirm_password (all required).
func (s *Server) handleSettingsAdminPasswordPost(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	// Validate new password.
	if err := validateNewPassword(newPassword); err != nil {
		s.renderAdminPasswordError(w, r, s.Secure, s.PanelPath, err.Error())
		return
	}

	// Validate passwords match.
	if newPassword != confirmPassword {
		s.renderAdminPasswordError(w, r, s.Secure, s.PanelPath, "passwords do not match")
		return
	}

	// Load current password hash.
	row := s.DB.QueryRow(`SELECT password_hash FROM admin WHERE id = 1`)
	var hash string
	if err := row.Scan(&hash); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Verify current password.
	if !CheckPassword(hash, currentPassword) {
		s.renderAdminPasswordError(w, r, s.Secure, s.PanelPath, "current password is incorrect")
		return
	}

	// Hash new password.
	newHash, err := HashPassword(newPassword)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Capture admin ID before invalidating sessions.
	adminID := s.sessionAdminID(r)

	// Update password in DB.
	if _, err := s.DB.Exec(`UPDATE admin SET password_hash = ? WHERE id = 1`, newHash); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Invalidate all sessions.
	if _, err := s.DB.Exec(`DELETE FROM sessions`); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	audit.Log(s.DB, adminID, "admin.password_changed", "", "", clientIP(r)) //nolint:errcheck

	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	adminPasswordPage(w, adminPasswordData{
		CSRFField: CSRFField(),
		CSRFToken: tok,
		Success:   "Password changed successfully.",
		PanelPath: s.PanelPath,
		Sessions:  s.loadAdminSessions(r),
	})
}

// handleSettingsSystemGet renders the system settings form.
func (s *Server) handleSettingsSystemGet(w http.ResponseWriter, r *http.Request) {
	tok, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)

	// Load current settings with defaults.
	panelPath := s.PanelPath
	logLevel := "info"
	retMinutes := 7
	retHourly := 30

	if val := s.DB.GetSetting(settingPanelPath, ""); val != "" {
		panelPath = val
	}
	if val := s.DB.GetSetting(settingLogLevel, "info"); val != "" {
		logLevel = val
	}
	if val := s.DB.GetSetting(settingRetentionMinuteDays, "7"); val != "" {
		if n, e := strconv.Atoi(val); e == nil {
			retMinutes = n
		}
	}
	if val := s.DB.GetSetting(settingRetentionHourlyDays, "30"); val != "" {
		if n, e := strconv.Atoi(val); e == nil {
			retHourly = n
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	systemSettingsPage(w, systemSettingsData{
		CSRFField:           CSRFField(),
		CSRFToken:           tok,
		PanelPath:           panelPath,
		LogLevel:            logLevel,
		RetentionMinuteDays: retMinutes,
		RetentionHourlyDays: retHourly,
		Success:             r.URL.Query().Get("notice"),
		Error:               r.URL.Query().Get("error"),
	})
}

// handleSettingsSystemPost processes system settings changes.
// Form fields: panel_path, log_level, retention_minutes, retention_hourly.
func (s *Server) handleSettingsSystemPost(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	panelPath := strings.TrimSpace(r.FormValue("panel_path"))
	logLevel := strings.TrimSpace(r.FormValue("log_level"))
	retMinStr := strings.TrimSpace(r.FormValue("retention_minutes"))
	retHourStr := strings.TrimSpace(r.FormValue("retention_hourly"))

	// Validate panel path.
	if err := validatePanelPath(panelPath); err != nil {
		renderSystemSettingsError(w, r, s.Secure, panelPath, logLevel, 0, 0, err.Error())
		return
	}

	// Validate log level.
	if !isValidLogLevel(logLevel) {
		renderSystemSettingsError(w, r, s.Secure, panelPath, logLevel, 0, 0, "invalid log level")
		return
	}

	// Validate retention minutes.
	retMin, err := strconv.Atoi(retMinStr)
	if err != nil || retMin < 1 || retMin > 30 {
		renderSystemSettingsError(w, r, s.Secure, panelPath, logLevel, 0, 0, "log retention (minutes) must be between 1 and 30 days")
		return
	}

	// Validate retention hourly.
	retHour, err := strconv.Atoi(retHourStr)
	if err != nil || retHour < 7 || retHour > 365 {
		renderSystemSettingsError(w, r, s.Secure, panelPath, logLevel, retMin, 0, "log retention (hourly) must be between 7 and 365 days")
		return
	}

	// Save all settings to DB.
	if err := s.DB.SetSetting(settingPanelPath, panelPath); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.DB.SetSetting(settingLogLevel, logLevel); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.DB.SetSetting(settingRetentionMinuteDays, retMinStr); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.DB.SetSetting(settingRetentionHourlyDays, retHourStr); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "settings.system", "", "", clientIP(r)) //nolint:errcheck

	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	systemSettingsPage(w, systemSettingsData{
		CSRFField:           CSRFField(),
		CSRFToken:           tok,
		PanelPath:           panelPath,
		LogLevel:            logLevel,
		RetentionMinuteDays: retMin,
		RetentionHourlyDays: retHour,
		Success:             "System settings saved. Panel path and log level changes require restarting tgproxy-panel.",
	})
}

// handleSettingsRestartServices restarts teleproxy and nginx services.
func (s *Server) handleSettingsRestartServices(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	var err error
	if !s.DevMode {
		if e := systemctlRun("restart", "teleproxy.service"); e != nil {
			err = e
		}
		if e := systemctlRun("restart", "nginx"); e != nil && err == nil {
			err = e
		}
	}

	audit.Log(s.DB, s.sessionAdminID(r), "settings.restart_services", "", "teleproxy, nginx", clientIP(r)) //nolint:errcheck

	if err != nil {
		http.Redirect(w, r, s.PanelPath+"settings/system?error="+url.QueryEscape("Failed to restart services: "+err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, s.PanelPath+"settings/system?notice="+url.QueryEscape("Services restarted."), http.StatusSeeOther)
}

// --- validation helpers ---

// validateNewPassword checks that password is ≥16 chars and contains letters and digits.
func validateNewPassword(pw string) error {
	if len(pw) < 16 {
		return fmt.Errorf("password must be at least 16 characters")
	}
	hasLetter := false
	hasDigit := false
	for _, r := range pw {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("password must contain both letters and digits")
	}
	return nil
}

// validatePanelPath checks that path starts and ends with /, is ≥6 chars, and
// contains no traversal sequences.
func validatePanelPath(p string) error {
	if !strings.HasPrefix(p, "/") || !strings.HasSuffix(p, "/") {
		return fmt.Errorf("panel path must start and end with /")
	}
	if len(p) < 6 {
		return fmt.Errorf("panel path must be at least 6 characters")
	}
	if strings.Contains(p, "..") || strings.Contains(p, "//") {
		return fmt.Errorf("panel path must not contain .. or //")
	}
	return nil
}

// isValidLogLevel checks if the log level is one of the allowed values.
func isValidLogLevel(level string) bool {
	return level == "debug" || level == "info" || level == "warn" || level == "error"
}

func isValidMaskHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func isValidWildcardMask(host string) bool {
	if !strings.HasPrefix(host, "*.") {
		return false
	}
	return isValidMaskHost(strings.TrimPrefix(host, "*."))
}

func isValidTLSBackend(backend string) bool {
	if strings.HasPrefix(backend, "unix:") {
		path := strings.TrimPrefix(backend, "unix:")
		return strings.HasPrefix(path, "/") && !strings.ContainsAny(path, "\r\n\"")
	}
	if strings.ContainsAny(backend, "\r\n\"") {
		return false
	}
	host, port, ok := strings.Cut(backend, ":")
	if !ok || host == "" || port == "" {
		return false
	}
	if _, err := strconv.Atoi(port); err != nil {
		return false
	}
	if isValidMaskHost(host) {
		return true
	}
	return isValidIPv4(host)
}

func isValidIPv4(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

// --- render error helpers ---

func (s *Server) renderProxySettingsError(w http.ResponseWriter, r *http.Request, secure bool, panelPath, maskHost, tlsBackend, wildcardMask string, mssClamp, randomPadding, ja4Log bool, port int, serverAddr, errMsg string) {
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, secure, panelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	proxySettingsPage(w, proxySettingsData{
		CSRFField:     CSRFField(),
		CSRFToken:     tok,
		MaskHost:      maskHost,
		TLSBackend:    tlsBackend,
		WildcardMask:  wildcardMask,
		MSSClamp:      mssClamp,
		RandomPadding: randomPadding,
		JA4Log:        ja4Log,
		MTProtoPort:   port,
		ServerAddr:    serverAddr,
		Error:         errMsg,
		PanelPath:     panelPath,
	})
}

func (s *Server) renderAdminPasswordError(w http.ResponseWriter, r *http.Request, secure bool, panelPath, errMsg string) {
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, secure, panelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	adminPasswordPage(w, adminPasswordData{
		CSRFField: CSRFField(),
		CSRFToken: tok,
		Error:     errMsg,
		PanelPath: panelPath,
		Sessions:  s.loadAdminSessions(r),
	})
}

func renderSystemSettingsError(w http.ResponseWriter, r *http.Request, secure bool, panelPath, logLevel string, retMin, retHour int, errMsg string) {
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, secure, panelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if retMin == 0 {
		retMin = 7
	}
	if retHour == 0 {
		retHour = 30
	}

	systemSettingsPage(w, systemSettingsData{
		CSRFField:           CSRFField(),
		CSRFToken:           tok,
		PanelPath:           panelPath,
		LogLevel:            logLevel,
		RetentionMinuteDays: retMin,
		RetentionHourlyDays: retHour,
		Error:               errMsg,
	})
}
