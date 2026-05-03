package panel

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
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
}

// Handler returns the root http.Handler. All requests outside PanelPath return 404.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// 404 for everything outside the configured panel path.
	panelPath := strings.TrimSuffix(s.PanelPath, "/")
	r.Mount(panelPath, s.panelRouter())

	// Catch-all: return 404 so no information is leaked outside the configured path.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	return r
}

func (s *Server) panelRouter() http.Handler {
	r := chi.NewRouter()

	r.Get("/login", s.handleLoginForm)
	r.Post("/login", s.handleLoginSubmit)
	r.Post("/logout", s.handleLogout)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.handleDashboard)
		r.Get("/dashboard", s.handleDashboard)
		r.Get("/users", s.handleUserList)
		r.Post("/users/create", s.handleUserCreate)
		r.Post("/users/{id}/toggle", s.handleUserToggle)
		r.Post("/users/{id}/rotate", s.handleUserRotate)
		r.Post("/users/{id}/delete", s.handleUserDelete)
		r.Get("/bridge", s.handleBridgePage)
		r.Post("/bridge/enable", s.handleBridgeEnable)
		r.Post("/bridge/disable", s.handleBridgeDisable)
		r.Post("/bridge/nodes/add", s.handleBridgeAddNode)
		r.Post("/bridge/nodes/add-manual", s.handleBridgeAddNodeManual)
		r.Get("/bridge/nodes/{id}/edit", s.handleBridgeEditNodeForm)
		r.Post("/bridge/nodes/{id}/edit", s.handleBridgeEditNode)
		r.Post("/bridge/nodes/{id}/ping", s.handleBridgePingNode)
		r.Post("/bridge/nodes/{id}/toggle", s.handleBridgeToggleNode)
		r.Post("/bridge/nodes/{id}/delete", s.handleBridgeDeleteNode)
		r.Post("/bridge/strategy", s.handleBridgeSetStrategy)
		r.Get("/settings/stubs", s.handleSettingsStubList)
		r.Post("/settings/stubs/apply", s.handleSettingsStubApply)
		r.Post("/settings/stubs/upload", s.handleSettingsStubUpload)
		r.Get("/settings/certificates", s.handleSettingsCertificates)
		r.Post("/settings/certificates/renew", s.handleSettingsCertRenew)
		r.Get("/settings/proxy", s.handleSettingsProxyGet)
		r.Post("/settings/proxy", s.handleSettingsProxyPost)
		r.Get("/settings/admin-password", s.handleSettingsAdminPasswordGet)
		r.Post("/settings/admin-password", s.handleSettingsAdminPasswordPost)
		r.Get("/settings/system", s.handleSettingsSystemGet)
		r.Post("/settings/system", s.handleSettingsSystemPost)
		r.Get("/logs", s.handleLogsPage)
		r.Get("/logs/stream", s.handleLogsStream)
		r.Get("/logs/download", s.handleLogsDownload)
		r.Get("/audit", s.handleAuditLog)
	})

	return r
}
