package panel

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/version"
)

func TestDashboardPageIncludesComponentsSection(t *testing.T) {
	data := DashboardData{
		Components: collectComponentVersions(),
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	if !strings.Contains(html, "Components") {
		t.Error("dashboard HTML should contain 'Components' section heading")
	}
	if !strings.Contains(html, version.Version) {
		t.Errorf("dashboard HTML should contain panel version %q", version.Version)
	}
}

func TestCollectComponentVersionsReturnsAllComponents(t *testing.T) {
	components := collectComponentVersions()

	wantNames := []string{"tgproxy-cli", "tgproxy-panel", "teleproxy", "sing-box"}
	if len(components) != len(wantNames) {
		t.Fatalf("got %d components, want %d", len(components), len(wantNames))
	}
	for i, want := range wantNames {
		if components[i].Name != want {
			t.Errorf("component[%d].Name = %q, want %q", i, components[i].Name, want)
		}
		if components[i].Version == "" {
			t.Errorf("component[%d].Version is empty, want non-empty string", i)
		}
	}
}

func TestCollectComponentVersionsSelfVersionMatchesPackage(t *testing.T) {
	components := collectComponentVersions()

	for _, c := range components {
		if c.Name == "tgproxy-cli" || c.Name == "tgproxy-panel" {
			if c.Version != version.Version {
				t.Errorf("component %q version = %q, want %q", c.Name, c.Version, version.Version)
			}
		}
	}
}

func TestCollectComponentVersionsUnknownForMissingBinaries(t *testing.T) {
	components := collectComponentVersions()

	for _, c := range components {
		if c.Name == "teleproxy" || c.Name == "sing-box" {
			// In test environment these binaries are not installed;
			// version must be "unknown" (not empty, not a panic).
			if c.Version == "" {
				t.Errorf("component %q version is empty; want 'unknown' or actual version", c.Name)
			}
		}
	}
}

// TestDashboardSingleModeRendersServicesSection verifies that Single mode
// (IsBridge = false) renders the Services table and not the Bridge steps table.
func TestDashboardSingleModeRendersServicesSection(t *testing.T) {
	data := DashboardData{
		IsBridge: false,
		Services: []health.ServiceState{
			{Name: "teleproxy.service", Active: true, Message: "running"},
			{Name: "nginx-stub", Active: false, Message: "loopback probe failed"},
		},
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	if !strings.Contains(html, "Services") {
		t.Error("Single mode dashboard should contain 'Services' heading")
	}
	if !strings.Contains(html, "teleproxy.service") {
		t.Error("Single mode dashboard should list teleproxy.service")
	}
	if strings.Contains(html, "Bridge Mode") {
		t.Error("Single mode dashboard should not contain 'Bridge Mode' heading")
	}
	if strings.Contains(html, "Chain Health") {
		t.Error("Single mode dashboard should not contain 'Chain Health' heading")
	}
}

// TestDashboardBridgeModeRendersBridgeSteps verifies that Bridge mode
// (IsBridge = true) renders the chain steps table and not the Services table.
func TestDashboardBridgeModeRendersBridgeSteps(t *testing.T) {
	data := DashboardData{
		IsBridge: true,
		BridgeSteps: []health.BridgeStepStatus{
			{Name: "teleproxy.service", OK: true, Message: "running"},
			{Name: "sing-box.service", OK: true, Message: "running"},
			{Name: "socks5-inbound", OK: true, Message: "reachable"},
			{Name: "telegram-chain", OK: false, Message: "chain unreachable: timeout"},
		},
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	if !strings.Contains(html, "Bridge Mode") {
		t.Error("Bridge mode dashboard should contain 'Bridge Mode' heading")
	}
	if !strings.Contains(html, "Chain Health") {
		t.Error("Bridge mode dashboard should contain 'Chain Health' heading")
	}
	if !strings.Contains(html, "sing-box.service") {
		t.Error("Bridge mode dashboard should list sing-box.service step")
	}
	if !strings.Contains(html, "telegram-chain") {
		t.Error("Bridge mode dashboard should list telegram-chain step")
	}
	// Services table heading must not appear (it belongs to Single mode only).
	if strings.Contains(html, "<h2>Services</h2>") {
		t.Error("Bridge mode dashboard should not contain Single-mode Services table")
	}
}

// TestDashboardLiveConnectionsSection verifies that the Active Connections
// section is rendered when LiveConnections data is present.
func TestDashboardLiveConnectionsSection(t *testing.T) {
	data := DashboardData{
		LiveConnections: []metrics.Sample{
			{UserLabel: "alice", Connections: 3},
			{UserLabel: "bob", Connections: 1},
		},
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	if !strings.Contains(html, "Active Connections") {
		t.Error("dashboard should contain 'Active Connections' heading when data is present")
	}
	if !strings.Contains(html, "alice") {
		t.Error("dashboard should list user 'alice' in Active Connections")
	}
	if !strings.Contains(html, "bob") {
		t.Error("dashboard should list user 'bob' in Active Connections")
	}
}

// TestDashboardNoLiveConnectionsNoSection verifies that the Active Connections
// section is absent when no live data is available.
func TestDashboardNoLiveConnectionsNoSection(t *testing.T) {
	data := DashboardData{
		LiveConnections: nil,
		Components:      []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	if strings.Contains(html, "Active Connections") {
		t.Error("dashboard should not show 'Active Connections' section when no live data")
	}
}

func TestDashboardTopUsersFormatsTrafficBytes(t *testing.T) {
	data := DashboardData{
		TopUsers: []metrics.UserTraffic{
			{UserLabel: "alice", BytesIn: 1024, BytesOut: 2 * 1024 * 1024, Connections: 2},
		},
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	if !strings.Contains(html, "1.0 KB") {
		t.Error("dashboard should format bytes in with binary units")
	}
	if !strings.Contains(html, "2.0 MB") {
		t.Error("dashboard should format bytes out with binary units")
	}
}
