package panel

import (
	"net/http"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
)

// DashboardData holds what the dashboard template receives.
type DashboardData struct {
	Services []health.ServiceState
	Period   metrics.Period
	TopUsers []metrics.UserTraffic
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	period := metrics.ParsePeriod(r.URL.Query().Get("period"))

	checker := health.DefaultChecker()
	status := checker.CheckSingle()

	topUsers, _ := metrics.QueryTopUsers(s.DB, period, 5, nil)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	dashboardPage(w, DashboardData{
		Services: status.Services,
		Period:   period,
		TopUsers: topUsers,
	})
}
