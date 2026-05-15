package panel

import (
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
)

func (s *Server) collectDashboardData(period metrics.Period) DashboardData {
	checker := health.DefaultChecker()
	isBridge := s.isBridgeMode()

	var services []health.ServiceState
	var bridgeSteps []health.BridgeStepStatus
	if isBridge {
		bridgeSteps = checker.CheckBridge().Steps
	} else {
		services = checker.CheckSingle().Services
	}

	topUsers, _ := metrics.QueryTopUsers(s.DB, period, maxActiveUsers, nil)

	return DashboardData{
		Services:        services,
		BridgeSteps:     bridgeSteps,
		IsBridge:        isBridge,
		PanelPath:       s.PanelPath,
		Period:          period,
		TopUsers:        topUsers,
		LiveConnections: s.scrapeLiveConnections(),
		Components:      collectComponentVersions(),
	}
}
