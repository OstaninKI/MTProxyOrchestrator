package panel

import (
	"net/http"
	"os/exec"
	"strings"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/version"
)

// ComponentVersion holds the name and installed version of a component.
type ComponentVersion struct {
	Name    string
	Version string // "unknown" if binary not found or command fails
}

// DashboardData holds what the dashboard template receives.
type DashboardData struct {
	Services   []health.ServiceState
	Period     metrics.Period
	TopUsers   []metrics.UserTraffic
	Components []ComponentVersion
}

// collectComponentVersions returns installed versions for all known components.
func collectComponentVersions() []ComponentVersion {
	paths := config.DefaultPaths()

	teleproxyVersion := "unknown"
	if out, err := exec.Command(paths.TeleproxyBin, "--version").Output(); err == nil {
		teleproxyVersion = strings.TrimSpace(string(out))
	}

	singboxVersion := "unknown"
	if out, err := exec.Command(paths.SingboxBin, "version").Output(); err == nil {
		lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
		if len(lines) > 0 {
			singboxVersion = strings.TrimSpace(lines[0])
		}
	}

	return []ComponentVersion{
		{Name: "tgproxy-cli", Version: version.Version},
		{Name: "tgproxy-panel", Version: version.Version},
		{Name: "teleproxy", Version: teleproxyVersion},
		{Name: "sing-box", Version: singboxVersion},
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	period := metrics.ParsePeriod(r.URL.Query().Get("period"))

	checker := health.DefaultChecker()
	status := checker.CheckSingle()

	topUsers, _ := metrics.QueryTopUsers(s.DB, period, 5, nil)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	dashboardPage(w, DashboardData{
		Services:   status.Services,
		Period:     period,
		TopUsers:   topUsers,
		Components: collectComponentVersions(),
	})
}
