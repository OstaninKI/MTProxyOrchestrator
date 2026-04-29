package panel

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/secrets"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
)

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
	http.Redirect(w, r, "../users", http.StatusSeeOther)
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
		http.Error(w, "failed to apply teleproxy config", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "../users", http.StatusSeeOther)
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
		}
		http.Error(w, "failed to apply teleproxy config", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	userCreatedPage(w, label, secret.Hex())
}

// reloadTeleproxy rewrites the Teleproxy config and reloads the service.
func (s *Server) reloadTeleproxy() error {
	users, err := UserRepo{DB: s.DB}.List()
	if err != nil {
		return err
	}
	var entries []teleproxy.UserEntry
	for _, u := range users {
		if u.Enabled && u.DeletedAt == nil {
			entries = append(entries, teleproxy.UserEntry{Label: u.Label, Secret: u.SecretHex})
		}
	}
	cfg := teleproxy.Config{
		Port:      s.bridgeMTProtoPort(),
		MaskHost:  s.bridgeMaskHost(),
		StatsPort: s.bridgeStatsPort(),
		Users:     entries,
	}
	data := cfg.Render()
	return writeAndReload(s.bridgePaths().TeleproxyTOML, data)
}

// writeAndReload is a hook for tests; production writes to the real path.
var writeAndReload = func(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	ctrl := teleproxy.DefaultController()
	return ctrl.Reload()
}
