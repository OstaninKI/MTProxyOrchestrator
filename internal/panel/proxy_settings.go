package panel

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
)

// Setting key constants for the settings table.
const (
	settingMaskHost            = "mask_host"
	settingMTProtoPort         = "mtproto_port"
	settingPanelPath           = "panel_path"
	settingLogLevel            = "log_level"
	settingRetentionMinuteDays = "retention_minutes_days"
	settingRetentionHourlyDays = "retention_hourly_days"
)

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
		CSRFField:   CSRFField(),
		CSRFToken:   tok,
		MaskHost:    s.bridgeMaskHost(),
		MTProtoPort: s.bridgeMTProtoPort(),
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
	portStr := strings.TrimSpace(r.FormValue("mtproto_port"))

	// Validate mask_host.
	if maskHost == "" {
		renderProxySettingsError(w, r, s.Secure, s.PanelPath, maskHost, s.bridgeMTProtoPort(), "mask host is required")
		return
	}
	if !isValidMaskHost(maskHost) {
		renderProxySettingsError(w, r, s.Secure, s.PanelPath, maskHost, s.bridgeMTProtoPort(), "mask host must be a valid hostname")
		return
	}

	// Validate port.
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		renderProxySettingsError(w, r, s.Secure, s.PanelPath, maskHost, s.bridgeMTProtoPort(), "port must be between 1 and 65535")
		return
	}

	// Save to DB.
	if err := s.DB.SetSetting(settingMaskHost, maskHost); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.DB.SetSetting(settingMTProtoPort, portStr); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Update runtime config.
	if s.BridgeCfg != nil {
		s.BridgeCfg.MaskHost = maskHost
		s.BridgeCfg.MTProtoPort = port
	}

	// Reload Teleproxy.
	if err := s.reloadTeleproxy(); err != nil {
		renderProxySettingsError(w, r, s.Secure, s.PanelPath, maskHost, port, "failed to reload teleproxy")
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "settings.proxy", "", "", clientIP(r)) //nolint:errcheck

	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	proxySettingsPage(w, proxySettingsData{
		CSRFField:   CSRFField(),
		CSRFToken:   tok,
		MaskHost:    maskHost,
		MTProtoPort: port,
		Success:     "Proxy settings saved.",
	})
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
		renderAdminPasswordError(w, r, s.Secure, s.PanelPath, err.Error())
		return
	}

	// Validate passwords match.
	if newPassword != confirmPassword {
		renderAdminPasswordError(w, r, s.Secure, s.PanelPath, "passwords do not match")
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
		renderAdminPasswordError(w, r, s.Secure, s.PanelPath, "current password is incorrect")
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

// --- render error helpers ---

func renderProxySettingsError(w http.ResponseWriter, r *http.Request, secure bool, panelPath, maskHost string, port int, errMsg string) {
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, secure, panelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	proxySettingsPage(w, proxySettingsData{
		CSRFField:   CSRFField(),
		CSRFToken:   tok,
		MaskHost:    maskHost,
		MTProtoPort: port,
		Error:       errMsg,
	})
}

func renderAdminPasswordError(w http.ResponseWriter, r *http.Request, secure bool, panelPath, errMsg string) {
	tok, _ := NewCSRFToken()
	SetCSRFCookie(w, tok, secure, panelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	adminPasswordPage(w, adminPasswordData{
		CSRFField: CSRFField(),
		CSRFToken: tok,
		Error:     errMsg,
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
