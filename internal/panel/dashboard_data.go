package panel

import (
	"log/slog"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
)

var dashboardHealthChecker = health.DefaultChecker

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
