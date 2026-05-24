package panel

import (
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	panelassets "github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel/assets"
)

// Server holds the HTTP server and its dependencies.
type Server struct {
	DB            *db.DB
	PanelPath     string // e.g. "/p-a8f3k2x9/"
	RateLimiter   *RateLimiter
	Secure        bool            // set false in tests; true in production (HTTPS)
	BridgeCfg     *BridgeConfig   // nil → use DefaultPaths and default ports
	SettingsCfg   *SettingsConfig // nil → empty stub/cert config
	SingboxActive func() bool     // lets tests stub the sing-box running check; nil = use real systemd
	// BridgeExec is the executor for bridge OS operations.
	// nil means use realBridgeExecutor{} (production default).
	BridgeExec bridge.Executor
	// DevMode disables all system side-effects (file writes, systemd, ACME).
	// Set by ApplyDevMode; never set in production.
	DevMode bool
	// RecalcUser, when set, is invoked after admin handlers mutate quota state
	// so the next periodic tick cannot surprise the just-saved row. Receives
	// the user label.
	RecalcUser func(label string)

	totpMu      sync.Mutex
	totpPending *pendingTOTPStore
}

// Handler returns the root http.Handler. All requests outside PanelPath return 404.
func (s *Server) Handler() http.Handler {
	s.PanelPath = normalizePanelPath(s.PanelPath)
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// 404 for everything outside the configured panel path.
	panelPath := panelMountPath(s.PanelPath)
	r.Mount(panelPath, s.panelRouter())

	// Catch-all: return 404 so no information is leaked outside the configured path.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	return r
}

func normalizePanelPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}

func panelMountPath(path string) string {
	path = strings.TrimSuffix(normalizePanelPath(path), "/")
	if path == "" {
		return "/"
	}
	return path
}

// secureHeaders sets security-related response headers on every panel response.
// These mirror the headers already set by the nginx reverse proxy so they are
// present even when the panel is accessed directly (development, direct TCP).
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func setStrictPanelCSP(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none';")
}

func setLegacyPanelCSP(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' wss:; frame-ancestors 'none';")
}

func withLegacyPanelCSP(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setLegacyPanelCSP(w)
		next(w, r)
	}
}

func (s *Server) panelRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(secureHeaders)

	r.Get("/login", s.handleLoginForm)
	r.Post("/login", s.handleLoginSubmit)
	r.Get("/totp/verify", withLegacyPanelCSP(s.handleTOTPVerifyForm))
	r.Post("/totp/verify", withLegacyPanelCSP(s.handleTOTPVerifySubmit))
	r.Post("/logout", s.handleLogout)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	r.Handle("/assets/*", http.StripPrefix(strings.TrimSuffix(s.PanelPath, "/")+"/assets/", panelassets.Handler()))

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.handleDashboard)
		r.Get("/dashboard", s.handleDashboard)
		r.Get("/dashboard/fragments/{name}", s.handleDashboardFragment)
		r.Get("/dashboard/events", s.handleDashboardEvents)
		r.Get("/users", withLegacyPanelCSP(s.handleUserList))
		r.Post("/users/create", withLegacyPanelCSP(s.handleUserCreate))
		r.Post("/users/rotate-all", withLegacyPanelCSP(s.handleUserRotateAll))
		r.Post("/users/bulk/suspend", withLegacyPanelCSP(s.handleUserBulkSuspend))
		r.Post("/users/bulk/rotate", withLegacyPanelCSP(s.handleUserBulkRotate))
		r.Post("/users/bulk/delete", withLegacyPanelCSP(s.handleUserBulkDelete))
		r.Post("/users/{id}/toggle", withLegacyPanelCSP(s.handleUserToggle))
		r.Post("/users/{id}/rotate", withLegacyPanelCSP(s.handleUserRotate))
		r.Post("/users/{id}/delete", withLegacyPanelCSP(s.handleUserDelete))
		r.Post("/users/{id}/quota", withLegacyPanelCSP(s.handleUserQuotaSet))
		r.Post("/users/{id}/quota/reset", withLegacyPanelCSP(s.handleUserQuotaReset))
		r.Post("/users/{id}/suspend", withLegacyPanelCSP(s.handleUserSuspendToggle))
		r.Post("/users/{id}/link", withLegacyPanelCSP(s.handleUserShareLink))
		r.Post("/users/{id}/qr", withLegacyPanelCSP(s.handleUserShareQR))
		r.Get("/bridge", withLegacyPanelCSP(s.handleBridgePage))
		r.Post("/bridge/enable", withLegacyPanelCSP(s.handleBridgeEnable))
		r.Post("/bridge/disable", withLegacyPanelCSP(s.handleBridgeDisable))
		r.Post("/bridge/nodes/add", withLegacyPanelCSP(s.handleBridgeAddNode))
		r.Post("/bridge/nodes/add-manual", withLegacyPanelCSP(s.handleBridgeAddNodeManual))
		r.Get("/bridge/nodes/{id}/edit", withLegacyPanelCSP(s.handleBridgeEditNodeForm))
		r.Post("/bridge/nodes/{id}/edit", withLegacyPanelCSP(s.handleBridgeEditNode))
		r.Post("/bridge/nodes/{id}/ping", withLegacyPanelCSP(s.handleBridgePingNode))
		r.Post("/bridge/nodes/{id}/toggle", withLegacyPanelCSP(s.handleBridgeToggleNode))
		r.Post("/bridge/nodes/{id}/delete", withLegacyPanelCSP(s.handleBridgeDeleteNode))
		r.Post("/bridge/strategy", withLegacyPanelCSP(s.handleBridgeSetStrategy))
		r.Get("/settings/stubs", withLegacyPanelCSP(s.handleSettingsStubList))
		r.Post("/settings/stubs/apply", withLegacyPanelCSP(s.handleSettingsStubApply))
		r.Post("/settings/stubs/upload", withLegacyPanelCSP(s.handleSettingsStubUpload))
		r.Get("/settings/stubs/remote", withLegacyPanelCSP(s.handleSettingsStubRemote))
		r.Post("/settings/stubs/remote-apply", withLegacyPanelCSP(s.handleSettingsStubRemoteApply))
		r.Get("/settings/certificates", s.handleSettingsCertificates)
		r.Post("/settings/certificates/config", withLegacyPanelCSP(s.handleSettingsCertRenewalConfig))
		r.Post("/settings/certificates/renew", withLegacyPanelCSP(s.handleSettingsCertRenew))
		r.Get("/settings/proxy", withLegacyPanelCSP(s.handleSettingsProxyGet))
		r.Post("/settings/proxy", withLegacyPanelCSP(s.handleSettingsProxyPost))
		r.Post("/settings/sessions/{id}/revoke", withLegacyPanelCSP(s.handleSettingsSessionRevoke))
		r.Get("/settings/admin-password", withLegacyPanelCSP(s.handleSettingsAdminPasswordGet))
		r.Post("/settings/admin-password", withLegacyPanelCSP(s.handleSettingsAdminPasswordPost))
		r.Get("/settings/system", withLegacyPanelCSP(s.handleSettingsSystemGet))
		r.Post("/settings/system", withLegacyPanelCSP(s.handleSettingsSystemPost))
		r.Post("/settings/system/restart", withLegacyPanelCSP(s.handleSettingsRestartServices))
		r.Get("/settings/totp", withLegacyPanelCSP(s.handleSettingsTOTPGet))
		r.Post("/settings/totp/begin", withLegacyPanelCSP(s.handleSettingsTOTPBegin))
		r.Post("/settings/totp/confirm", withLegacyPanelCSP(s.handleSettingsTOTPConfirm))
		r.Post("/settings/totp/disable", withLegacyPanelCSP(s.handleSettingsTOTPDisable))
		r.Post("/settings/totp/regenerate", withLegacyPanelCSP(s.handleSettingsTOTPRegenerate))
		r.Get("/logs", withLegacyPanelCSP(s.handleLogsPage))
		r.Get("/logs/stream", withLegacyPanelCSP(s.handleLogsStream))
		r.Get("/logs/download", withLegacyPanelCSP(s.handleLogsDownload))
		r.Get("/audit", s.handleAuditLog)
	})

	return r
}
