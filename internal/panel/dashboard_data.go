package panel

import (
	"log/slog"
	"strings"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
)

var dashboardHealthChecker = health.DefaultChecker

// serviceStatesFromBridgeSteps maps the systemd-unit bridge steps (e.g.
// teleproxy.service, sing-box.service) to ServiceState rows so the
// "Services & Components" table lists them in Bridge mode too. Connectivity
// probes (socks5-inbound, telegram-chain) are not systemd units and are
// deliberately left out of the table.
func serviceStatesFromBridgeSteps(steps []health.BridgeStepStatus) []health.ServiceState {
	var services []health.ServiceState
	for _, step := range steps {
		if !strings.HasSuffix(step.Name, ".service") {
			continue
		}
		services = append(services, health.ServiceState{
			Name:    step.Name,
			Active:  step.OK,
			Message: step.Message,
		})
	}
	return services
}

func (s *Server) collectDashboardData(period metrics.Period) DashboardData {
	checker := dashboardHealthChecker()
	isBridge := s.isBridgeMode()

	var services []health.ServiceState
	var bridgeSteps []health.BridgeStepStatus
	healthy := false
	healthLabel := "unhealthy"
	if isBridge {
		status := checker.CheckBridge()
		bridgeSteps = status.Steps
		// Surface the systemd-unit steps in the Services & Components table so
		// it is not empty of services in Bridge mode (parity with Single mode).
		services = serviceStatesFromBridgeSteps(status.Steps)
		healthy = status.OK
		healthLabel = status.Summary
	} else {
		status := checker.CheckSingle()
		services = status.Services
		healthy = status.OK
		healthLabel = status.Summary
	}

	users, err := UserRepo{DB: s.DB}.List()
	if err != nil {
		slog.Warn("dashboard users query failed", "err", err)
	}
	topUsers, err := metrics.QueryTopUsers(s.DB, period, maxActiveUsers, nil)
	if err != nil {
		slog.Warn("dashboard top users query failed", "err", err)
	}
	trafficSeries, err := metrics.QueryTrafficSeries(s.DB, period, 60, nil)
	if err != nil {
		slog.Warn("dashboard traffic series query failed", "err", err)
	}
	opsSeries, err := metrics.QueryOpsSeries(s.DB, period, 60, nil)
	if err != nil {
		slog.Warn("dashboard ops series query failed", "err", err)
	}
	nodeList, err := bridge.Load(s.nodePath())
	if err != nil {
		slog.Warn("dashboard bridge node load failed", "err", err)
	}

	teleproxySnapshot := s.scrapeTeleproxySnapshot()

	return DashboardData{
		Services:        services,
		BridgeSteps:     bridgeSteps,
		IsBridge:        isBridge,
		Healthy:         healthy,
		HealthLabel:     healthLabel,
		PanelPath:       s.PanelPath,
		Period:          period,
		TopUsers:        topUsers,
		TrafficSeries:   trafficSeries,
		OpsSeries:       opsSeries,
		LiveConnections: teleproxySnapshot.Samples,
		Teleproxy:       teleproxySnapshot,
		Components:      collectComponentVersions(),
		Users:           users,
		BridgeNodes:     nodeList.Nodes,
		System:          collectSystemSnapshot(),
	}
}
