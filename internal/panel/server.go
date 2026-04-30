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
	DB          *db.DB
	PanelPath   string // e.g. "/p-a8f3k2x9/"
	RateLimiter *RateLimiter
	Secure      bool          // set false in tests; true in production (HTTPS)
	BridgeCfg   *BridgeConfig // nil → use DefaultPaths and default ports
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
		r.Post("/bridge/nodes/{id}/toggle", s.handleBridgeToggleNode)
		r.Post("/bridge/nodes/{id}/delete", s.handleBridgeDeleteNode)
		r.Get("/logs", s.handleLogsPage)
		r.Get("/logs/stream", s.handleLogsStream)
		r.Get("/logs/download", s.handleLogsDownload)
	})

	return r
}
