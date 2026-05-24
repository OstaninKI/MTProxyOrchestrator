package panel

import (
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/secrets"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
)

const (
	singboxSOCKSHost = "127.0.0.1"
	singboxSOCKSPort = 1080
)

// isSingboxActive reports whether the sing-box systemd service is currently active.
// It is a var so tests can replace it without touching the real system.
var isSingboxActive = func() bool {
	err := exec.Command("systemctl", "is-active", "sing-box.service").Run()
	return err == nil
}

// bridgeSOCKS5Addr returns the sing-box SOCKS5 address when Bridge mode is active,
// and an empty string when Single mode is active.
func (s *Server) bridgeSOCKS5Addr() string {
	if s.currentMode() == config.ModeBridge {
		return fmt.Sprintf("%s:%d", singboxSOCKSHost, singboxSOCKSPort)
	}
	return ""
}

func (s *Server) currentMode() config.Mode {
	mode, err := teleproxy.DetectMode(s.bridgePaths().TeleproxyTOML)
	if err == nil {
		return mode
	}
	if isSingboxActive() {
		return config.ModeBridge
	}
	return config.ModeSingle
}

func (s *Server) handleUserToggle(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	repo := UserRepo{DB: s.DB}
	users, err := repo.List()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var target *UserRow
	for i := range users {
		if users[i].ID == id {
			target = &users[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	newState := !target.Enabled
	if err := repo.SetEnabled(id, newState); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	action := "user.enable"
	if !newState {
		action = "user.disable"
	}
	audit.Log(s.DB, s.sessionAdminID(r), action, target.Label, "", clientIP(r)) //nolint:errcheck
	if err := s.reloadTeleproxy(); err != nil {
		_ = repo.SetEnabled(id, target.Enabled)
		http.Error(w, "failed to apply teleproxy config", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "../../users", http.StatusSeeOther)
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	repo := UserRepo{DB: s.DB}
	users, _ := repo.List()
	var label string
	for _, u := range users {
		if u.ID == id {
			label = u.Label
		}
	}
	if err := repo.Delete(id); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	audit.Log(s.DB, s.sessionAdminID(r), "user.delete", label, "", clientIP(r)) //nolint:errcheck
	if err := s.reloadTeleproxy(); err != nil {
		_ = repo.Restore(id)
		audit.Log(s.DB, s.sessionAdminID(r), "user.delete_rollback", label, "user restored after reload failure", clientIP(r)) //nolint:errcheck
		http.Error(w, "failed to apply teleproxy config", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "../../users", http.StatusSeeOther)
}

func (s *Server) handleUserRotate(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	secret, err := secrets.GenerateMTProtoSecret()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	repo := UserRepo{DB: s.DB}
	users, _ := repo.List()
	var label string
	var oldSecret string
	for _, u := range users {
		if u.ID == id {
			label = u.Label
			oldSecret = u.SecretHex
		}
	}
	if err := repo.UpdateSecret(id, secret.Hex()); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	audit.Log(s.DB, s.sessionAdminID(r), "user.rotate", label, "", clientIP(r)) //nolint:errcheck
	if err := s.reloadTeleproxy(); err != nil {
		if oldSecret != "" {
			_ = repo.UpdateSecret(id, oldSecret)
			audit.Log(s.DB, s.sessionAdminID(r), "user.rotate_rollback", label, "secret restored after reload failure", clientIP(r)) //nolint:errcheck
		}
		http.Error(w, "failed to apply teleproxy config", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tok, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	serverAddr := s.settingsConfig().Domain
	if serverAddr == "" {
		serverAddr = s.settingsConfig().ServerIP
	}
	userCreatedPage(w, label, secret.Hex(), serverAddr, s.bridgeMTProtoPort(), s.bridgeMaskHost(), s.PanelPath, tok)
}

// handleUserRotateAll re-issues secrets for every enabled user in a single
// teleproxy reload. On reload failure all rotated secrets are restored.
func (s *Server) handleUserRotateAll(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	repo := UserRepo{DB: s.DB}
	users, err := repo.List()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type prevSecret struct {
		id     int64
		secret string
	}
	var rotated []prevSecret
	restore := func() {
		for _, p := range rotated {
			_ = repo.UpdateSecret(p.id, p.secret)
		}
	}

	for _, u := range users {
		if !u.Enabled {
			continue
		}
		secret, err := secrets.GenerateMTProtoSecret()
		if err != nil {
			restore()
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := repo.UpdateSecret(u.ID, secret.Hex()); err != nil {
			restore()
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		rotated = append(rotated, prevSecret{id: u.ID, secret: u.SecretHex})
	}

	audit.Log(s.DB, s.sessionAdminID(r), "user.rotate_all", "", fmt.Sprintf("%d secrets", len(rotated)), clientIP(r)) //nolint:errcheck
	if err := s.reloadTeleproxy(); err != nil {
		restore()
		audit.Log(s.DB, s.sessionAdminID(r), "user.rotate_all_rollback", "", "secrets restored after reload failure", clientIP(r)) //nolint:errcheck
		http.Redirect(w, r, s.PanelPath+"users?error=Failed+to+apply+teleproxy+config", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.PanelPath+"users?notice="+url.QueryEscape(fmt.Sprintf("Rotated %d secret(s). Re-issue updated share links to users.", len(rotated))), http.StatusSeeOther)
}

// handleUserBulkSuspend suspends multiple selected users in one teleproxy reload.
// On reload failure all suspended users are restored.
func (s *Server) handleUserBulkSuspend(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		// Silently ignore parse error and treat as no ids
	}
	ids := r.PostForm["id"]
	if len(ids) == 0 {
		http.Redirect(w, r, s.PanelPath+"users", http.StatusSeeOther)
		return
	}

	repo := UserRepo{DB: s.DB}
	users, err := repo.List()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Parse IDs and build id → user map
	var parsedIDs []int64
	userMap := make(map[int64]*UserRow)
	for _, u := range users {
		userMap[u.ID] = &u
	}
	for _, idStr := range ids {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue // Skip invalid IDs
		}
		parsedIDs = append(parsedIDs, id)
	}

	if len(parsedIDs) == 0 {
		http.Redirect(w, r, s.PanelPath+"users", http.StatusSeeOther)
		return
	}

	type prevState struct {
		id        int64
		suspended bool
	}
	var changed []prevState
	restore := func() {
		for _, p := range changed {
			_ = repo.SetSuspended(p.id, p.suspended)
		}
	}

	// Apply suspension and track previous state
	for _, id := range parsedIDs {
		u, exists := userMap[id]
		if !exists {
			continue
		}
		changed = append(changed, prevState{id: id, suspended: u.QuotaSuspended})
		if err := repo.SetSuspended(id, true); err != nil {
			restore()
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	audit.Log(s.DB, s.sessionAdminID(r), "user.bulk_suspend", "", fmt.Sprintf("%d user(s)", len(changed)), clientIP(r)) //nolint:errcheck
	if err := s.reloadTeleproxy(); err != nil {
		restore()
		audit.Log(s.DB, s.sessionAdminID(r), "user.bulk_suspend_rollback", "", "users restored after reload failure", clientIP(r)) //nolint:errcheck
		http.Redirect(w, r, s.PanelPath+"users?error=Failed+to+apply+teleproxy+config", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.PanelPath+"users?notice="+url.QueryEscape(fmt.Sprintf("Suspended %d user(s).", len(changed))), http.StatusSeeOther)
}

// handleUserBulkRotate rotates secrets for multiple selected users in one teleproxy reload.
// On reload failure all rotated secrets are restored.
func (s *Server) handleUserBulkRotate(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		// Silently ignore parse error and treat as no ids
	}
	ids := r.PostForm["id"]
	if len(ids) == 0 {
		http.Redirect(w, r, s.PanelPath+"users", http.StatusSeeOther)
		return
	}

	repo := UserRepo{DB: s.DB}
	users, err := repo.List()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Parse IDs and build id → user map
	var parsedIDs []int64
	userMap := make(map[int64]*UserRow)
	for _, u := range users {
		userMap[u.ID] = &u
	}
	for _, idStr := range ids {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue // Skip invalid IDs
		}
		parsedIDs = append(parsedIDs, id)
	}

	if len(parsedIDs) == 0 {
		http.Redirect(w, r, s.PanelPath+"users", http.StatusSeeOther)
		return
	}

	type prevSecret struct {
		id     int64
		secret string
	}
	var rotated []prevSecret
	restore := func() {
		for _, p := range rotated {
			_ = repo.UpdateSecret(p.id, p.secret)
		}
	}

	// Generate and apply new secrets
	for _, id := range parsedIDs {
		u, exists := userMap[id]
		if !exists {
			continue
		}
		secret, err := secrets.GenerateMTProtoSecret()
		if err != nil {
			restore()
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		rotated = append(rotated, prevSecret{id: id, secret: u.SecretHex})
		if err := repo.UpdateSecret(id, secret.Hex()); err != nil {
			restore()
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	audit.Log(s.DB, s.sessionAdminID(r), "user.bulk_rotate", "", fmt.Sprintf("%d secret(s)", len(rotated)), clientIP(r)) //nolint:errcheck
	if err := s.reloadTeleproxy(); err != nil {
		restore()
		audit.Log(s.DB, s.sessionAdminID(r), "user.bulk_rotate_rollback", "", "secrets restored after reload failure", clientIP(r)) //nolint:errcheck
		http.Redirect(w, r, s.PanelPath+"users?error=Failed+to+apply+teleproxy+config", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.PanelPath+"users?notice="+url.QueryEscape(fmt.Sprintf("Rotated %d secret(s). Re-issue updated share links.", len(rotated))), http.StatusSeeOther)
}

// handleUserBulkDelete deletes multiple selected users in one teleproxy reload.
// On reload failure all deleted users are restored.
func (s *Server) handleUserBulkDelete(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		// Silently ignore parse error and treat as no ids
	}
	ids := r.PostForm["id"]
	if len(ids) == 0 {
		http.Redirect(w, r, s.PanelPath+"users", http.StatusSeeOther)
		return
	}

	repo := UserRepo{DB: s.DB}
	users, err := repo.List()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Parse IDs and build id → user map
	var parsedIDs []int64
	userMap := make(map[int64]*UserRow)
	for _, u := range users {
		userMap[u.ID] = &u
	}
	for _, idStr := range ids {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue // Skip invalid IDs
		}
		parsedIDs = append(parsedIDs, id)
	}

	if len(parsedIDs) == 0 {
		http.Redirect(w, r, s.PanelPath+"users", http.StatusSeeOther)
		return
	}

	restore := func() {
		for _, id := range parsedIDs {
			_ = repo.Restore(id)
		}
	}

	// Delete all selected users
	for _, id := range parsedIDs {
		if err := repo.Delete(id); err != nil {
			restore()
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	audit.Log(s.DB, s.sessionAdminID(r), "user.bulk_delete", "", fmt.Sprintf("%d user(s)", len(parsedIDs)), clientIP(r)) //nolint:errcheck
	if err := s.reloadTeleproxy(); err != nil {
		restore()
		audit.Log(s.DB, s.sessionAdminID(r), "user.bulk_delete_rollback", "", "users restored after reload failure", clientIP(r)) //nolint:errcheck
		http.Redirect(w, r, s.PanelPath+"users?error=Failed+to+apply+teleproxy+config", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.PanelPath+"users?notice="+url.QueryEscape(fmt.Sprintf("Deleted %d user(s).", len(parsedIDs))), http.StatusSeeOther)
}

// ReloadTeleproxyForQuota is exposed so the quota service can rebuild teleproxy
// config when a user's suspension state transitions.
func (s *Server) ReloadTeleproxyForQuota() error { return s.reloadTeleproxy() }

func (s *Server) handleUserQuotaSet(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	gb, err := strconv.ParseFloat(r.FormValue("gb"), 64)
	if err != nil || math.IsNaN(gb) || math.IsInf(gb, 0) {
		http.Error(w, "invalid quota size", http.StatusBadRequest)
		return
	}
	if gb < 0 {
		gb = 0
	}
	if gb > float64(math.MaxInt64)/(1024*1024*1024) {
		http.Error(w, "quota size too large", http.StatusBadRequest)
		return
	}
	bytes := int64(gb * 1024 * 1024 * 1024)
	period := r.FormValue("period")
	warnRaw := r.FormValue("warn_pct")
	warn := 80
	if warnRaw != "" {
		warn, err = strconv.Atoi(warnRaw)
		if err != nil {
			http.Error(w, "invalid warning percentage", http.StatusBadRequest)
			return
		}
	}
	repo := UserRepo{DB: s.DB}
	users, _ := repo.List()
	var target *UserRow
	for i := range users {
		if users[i].ID == id {
			target = &users[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	prev := *target
	if err := repo.SetQuota(id, bytes, period, warn, time.Now().Unix()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	audit.Log(s.DB, s.sessionAdminID(r), "user.quota_set", target.Label,
		fmt.Sprintf("bytes=%d period=%s warn=%d", bytes, period, warn), clientIP(r)) //nolint:errcheck
	if err := s.reloadTeleproxy(); err != nil {
		_ = repo.RestoreQuotaState(id, prev)
		http.Error(w, "failed to apply teleproxy config", http.StatusInternalServerError)
		return
	}
	if s.RecalcUser != nil {
		s.RecalcUser(target.Label)
	}
	http.Redirect(w, r, "../../users", http.StatusSeeOther)
}

func (s *Server) handleUserQuotaReset(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	repo := UserRepo{DB: s.DB}
	users, _ := repo.List()
	var target *UserRow
	for i := range users {
		if users[i].ID == id {
			target = &users[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	prev := *target
	if err := repo.ResetQuota(id, time.Now().Unix()); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	audit.Log(s.DB, s.sessionAdminID(r), "user.quota_reset", target.Label, "", clientIP(r)) //nolint:errcheck
	if err := s.reloadTeleproxy(); err != nil {
		_ = repo.RestoreQuotaState(id, prev)
		http.Error(w, "failed to apply teleproxy config", http.StatusInternalServerError)
		return
	}
	if s.RecalcUser != nil {
		s.RecalcUser(target.Label)
	}
	http.Redirect(w, r, "../../users", http.StatusSeeOther)
}

func (s *Server) handleUserSuspendToggle(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	repo := UserRepo{DB: s.DB}
	users, _ := repo.List()
	var target *UserRow
	for i := range users {
		if users[i].ID == id {
			target = &users[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	newState := !target.QuotaSuspended
	if err := repo.SetSuspended(id, newState); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	action := "user.suspend"
	if !newState {
		action = "user.unsuspend"
	}
	audit.Log(s.DB, s.sessionAdminID(r), action, target.Label, "", clientIP(r)) //nolint:errcheck
	if err := s.reloadTeleproxy(); err != nil {
		_ = repo.SetSuspended(id, target.QuotaSuspended)
		http.Error(w, "failed to apply teleproxy config", http.StatusInternalServerError)
		return
	}
	if s.RecalcUser != nil {
		s.RecalcUser(target.Label)
	}
	http.Redirect(w, r, "../../users", http.StatusSeeOther)
}

func (s *Server) handleUserShareLink(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	repo := UserRepo{DB: s.DB}
	users, err := repo.List()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var target *UserRow
	for i := range users {
		if users[i].ID == id {
			target = &users[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	serverAddr := s.settingsConfig().Domain
	if serverAddr == "" {
		serverAddr = s.settingsConfig().ServerIP
	}
	link := ProxyLink{
		Server:    serverAddr,
		Port:      s.bridgeMTProtoPort(),
		SecretHex: target.SecretHex,
		MaskHost:  s.bridgeMaskHost(),
	}
	url := link.TelegramURL()
	audit.Log(s.DB, s.sessionAdminID(r), "user.link_reveal", target.Label, "", clientIP(r)) //nolint:errcheck
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(url))
}

func (s *Server) handleUserShareQR(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	repo := UserRepo{DB: s.DB}
	users, err := repo.List()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var target *UserRow
	for i := range users {
		if users[i].ID == id {
			target = &users[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	serverAddr := s.settingsConfig().Domain
	if serverAddr == "" {
		serverAddr = s.settingsConfig().ServerIP
	}
	link := ProxyLink{
		Server:    serverAddr,
		Port:      s.bridgeMTProtoPort(),
		SecretHex: target.SecretHex,
		MaskHost:  s.bridgeMaskHost(),
	}
	png, err := link.QRPNG(256)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	b64 := base64.StdEncoding.EncodeToString(png)
	audit.Log(s.DB, s.sessionAdminID(r), "user.qr_reveal", target.Label, "", clientIP(r)) //nolint:errcheck
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(b64))
}

// reloadTeleproxy rewrites the Teleproxy config and reloads the service.
// It preserves Bridge mode by including the SOCKS5 upstream when sing-box is active.
func (s *Server) reloadTeleproxy() error {
	users, err := UserRepo{DB: s.DB}.List()
	if err != nil {
		return err
	}
	var entries []teleproxy.UserEntry
	for _, u := range users {
		if u.Enabled && u.DeletedAt == nil && !u.QuotaSuspended {
			entries = append(entries, teleproxy.UserEntry{Label: u.Label, Secret: u.SecretHex})
		}
	}
	usersData, err := teleproxy.MarshalUsersJSON(entries)
	if err != nil {
		return err
	}
	if err := SyncUsersJSONHook(s.bridgePaths().UsersJSON, usersData); err != nil {
		return err
	}
	cfg := teleproxy.Config{
		Port:       s.bridgeMTProtoPort(),
		MaskHost:   s.bridgeTeleproxyDomain(),
		StatsPort:  s.bridgeStatsPort(),
		SOCKS5Addr: s.bridgeSOCKS5Addr(),
		Users:      entries,
	}
	data := cfg.Render()
	return WriteAndReloadHook(s.bridgePaths().TeleproxyTOML, data)
}

// SyncUsersJSONHook is a hook for tests; production writes to the real path.
var SyncUsersJSONHook = func(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

// WriteAndReloadHook is a hook for tests; production writes to the real path.
var WriteAndReloadHook = func(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	ctrl := teleproxy.DefaultController()
	return ctrl.Reload()
}
