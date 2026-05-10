package panel

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/version"
)

// ComponentVersion holds the name and installed version of a component.
type ComponentVersion struct {
	Name    string
	Version string // "unknown" if binary not found or command fails
}

// DashboardData holds what the dashboard template receives.
type DashboardData struct {
	Services        []health.ServiceState
	BridgeSteps     []health.BridgeStepStatus // populated in Bridge mode
	IsBridge        bool
	PanelPath       string
	Period          metrics.Period
	TopUsers        []metrics.UserTraffic
	LiveConnections []metrics.Sample // latest per-user active connection counts
	Components      []ComponentVersion
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

// isBridgeMode returns true when Bridge mode is active, determined by the
// presence of at least one enabled node in outbounds.json.
func (s *Server) isBridgeMode() bool {
	if mode, err := teleproxy.DetectMode(s.bridgePaths().TeleproxyTOML); err == nil {
		return mode == config.ModeBridge
	}
	nl, err := bridge.Load(s.nodePath())
	if err != nil {
		return false
	}
	return len(nl.Active()) > 0
}

// statsAddr returns the Teleproxy metrics base URL, e.g. "http://127.0.0.1:9091".
func (s *Server) statsAddr() string {
	port := s.bridgeStatsPort()
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// scrapeLiveConnections fetches current per-user metrics from Teleproxy without
// persisting them. Returns nil on error (dashboard degrades gracefully).
func (s *Server) scrapeLiveConnections() []metrics.Sample {
	scraper := metrics.DefaultScraper(s.statsAddr())
	samples, err := scraper.Scrape()
	if err != nil {
		return nil
	}
	return samples
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	period := metrics.ParsePeriod(r.URL.Query().Get("period"))

	checker := health.DefaultChecker()
	isBridge := s.isBridgeMode()

	var services []health.ServiceState
	var bridgeSteps []health.BridgeStepStatus

	if isBridge {
		bs := checker.CheckBridge()
		bridgeSteps = bs.Steps
	} else {
		status := checker.CheckSingle()
		services = status.Services
	}

	topUsers, _ := metrics.QueryTopUsers(s.DB, period, 5, nil)
	liveConns := s.scrapeLiveConnections()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	dashboardPage(w, DashboardData{
		Services:        services,
		BridgeSteps:     bridgeSteps,
		IsBridge:        isBridge,
		PanelPath:       s.PanelPath,
		Period:          period,
		TopUsers:        topUsers,
		LiveConnections: liveConns,
		Components:      collectComponentVersions(),
	})
}
