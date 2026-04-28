package panel

import (
	"net/http"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
)

// DashboardData holds what the dashboard template receives.
type DashboardData struct {
	Services []health.ServiceState
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	checker := health.DefaultChecker()
	status := checker.CheckSingle()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	dashboardPage(w, DashboardData{Services: status.Services})
}
