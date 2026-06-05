package panel

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/version"
)

func TestDashboardPageIncludesComponentsSection(t *testing.T) {
	data := DashboardData{
		PanelPath:  "/p-example/",
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

func TestDashboardUsesLocalAssets(t *testing.T) {
	data := DashboardData{
		PanelPath:  "/p-example/",
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	for _, want := range []string{
		`href="/p-example/assets/panel.css`,
		`<meta name="htmx-config" content='{"includeIndicatorStyles":false}'>`,
		`src="/p-example/assets/vendor/htmx-2.0.10.min.js"`,
		`src="/p-example/assets/vendor/htmx-ext-sse-2.2.4.js"`,
		`src="/p-example/assets/panel.js`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard HTML missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<style>") || strings.Contains(html, "<script>(function()") {
		t.Fatalf("dashboard must not use inline CSS or inline CSRF script")
	}
	configIdx := strings.Index(html, `<meta name="htmx-config" content='{"includeIndicatorStyles":false}'>`)
	htmxIdx := strings.Index(html, `src="/p-example/assets/vendor/htmx-2.0.10.min.js"`)
	if configIdx == -1 || htmxIdx == -1 || configIdx > htmxIdx {
		t.Fatalf("dashboard must render htmx meta config before htmx bundle:\n%s", html)
	}
	if strings.Contains(html, "htmx-config.js") {
		t.Fatalf("dashboard HTML must not reference removed htmx-config.js asset:\n%s", html)
	}
}

func TestDashboardIncludesSSEFragmentRefreshMarkup(t *testing.T) {
	data := DashboardData{
		PanelPath:  "/p-example/",
		Period:     "24h",
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	for _, want := range []string{
		`hx-ext="sse"`,
		`sse-connect="/p-example/dashboard/events?period=24h"`,
		`sse-close="dashboard-close"`,
		`hx-get="/p-example/dashboard/fragments/health?period=24h"`,
		`hx-get="/p-example/dashboard/fragments/connections?period=24h"`,
		`hx-get="/p-example/dashboard/fragments/traffic?period=24h"`,
		`hx-get="/p-example/dashboard/fragments/components?period=24h"`,
		`hx-get="/p-example/dashboard/fragments/ops?period=24h"`,
		`hx-trigger="sse:dashboard-health"`,
		`hx-trigger="sse:dashboard-connections"`,
		`hx-trigger="sse:dashboard-traffic"`,
		`hx-trigger="sse:dashboard-components"`,
		`hx-trigger="sse:dashboard-ops"`,
		`sse-swap="dashboard-heartbeat"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard HTML missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<style>") || strings.Contains(html, "<script>(function()") {
		t.Fatalf("dashboard must not use inline CSS or inline CSRF script")
	}
}

func TestDashboardStatusStripIncludesHealthAndCounts(t *testing.T) {
	data := DashboardData{
		PanelPath:   "/p-example/",
		Period:      "24h",
		IsBridge:    true,
		HealthLabel: "All systems operational",
		System:      SystemSnapshot{Uptime: "3d 4h"},
		Users: []UserRow{
			{Label: "alice", Enabled: true},
			{Label: "bob", Enabled: false},
		},
		BridgeNodes: []bridge.Node{
			{Tag: "de-1", Enabled: true},
			{Tag: "nl-1", Enabled: true},
		},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	for _, want := range []string{
		`<span class="k">Mode</span>`,
		`Bridge`,
		`<span class="k">Uptime</span>`,
		`<span class="k">Heartbeat</span>`,
		`connecting`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard status strip missing %q:\n%s", want, html)
		}
	}
	if got := strings.Count(html, `<div class="item">`); got != 3 {
		t.Fatalf("dashboard status strip must render exactly 3 items, got %d:\n%s", got, html)
	}
}

func TestDashboardUsesSharedTopbarActiveState(t *testing.T) {
	data := DashboardData{
		PanelPath:  "/p-example/",
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	if !strings.Contains(html, `class="nav-item" data-active="true" href="/p-example/dashboard"`) {
		t.Fatalf("dashboard topbar must use shared active-state markup:\n%s", html)
	}
}

func TestDashboardRouteNormalizesPanelPathWithoutTrailingSlash(t *testing.T) {
	s := newDashboardTestServer(t)
	s.PanelPath = "/p-example"
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard?period=24h", nil)
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /dashboard status = %d, want 200", rec.Code)
	}
	html := rec.Body.String()
	for _, want := range []string{
		`href="/p-example/assets/panel.css`,
		`src="/p-example/assets/vendor/htmx-2.0.10.min.js"`,
		`src="/p-example/assets/vendor/htmx-ext-sse-2.2.4.js"`,
		`src="/p-example/assets/panel.js`,
		`sse-connect="/p-example/dashboard/events?period=24h"`,
		`hx-get="/p-example/dashboard/fragments/health?period=24h"`,
		`action="/p-example/logout"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard with panel path without trailing slash missing %q:\n%s", want, html)
		}
	}
	for _, bad := range []string{"/p-exampleassets/", "/p-exampledashboard/", "/p-examplelogout"} {
		if strings.Contains(html, bad) {
			t.Fatalf("dashboard contains malformed panel path %q:\n%s", bad, html)
		}
	}
}

func TestDashboardRouteRendersCSRFLogoutForm(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard?period=24h", nil)
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	html := rec.Body.String()
	if !strings.Contains(html, `action="/p-example/logout"`) {
		t.Fatalf("dashboard logout form missing panel-scoped action:\n%s", html)
	}
	if !strings.Contains(html, `name="_csrf" value="`) {
		t.Fatalf("dashboard logout form missing server-rendered CSRF value:\n%s", html)
	}
	if strings.Contains(html, `name="_csrf" class="js-csrf"><`) {
		t.Fatalf("dashboard logout form must not rely on JS-only CSRF injection:\n%s", html)
	}
	csrfCookie := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == csrfCookieName && cookie.Value != "" && cookie.Path == "/p-example/" {
			csrfCookie = true
		}
	}
	if !csrfCookie {
		t.Fatalf("dashboard response did not set panel-scoped CSRF cookie: %#v", rec.Result().Cookies())
	}
}

func TestHtmxMetaConfigPrecedesBundle(t *testing.T) {
	data := DashboardData{
		PanelPath:  "/p-example/",
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	meta := `<meta name="htmx-config" content='{"includeIndicatorStyles":false}'>`
	metaIdx := strings.Index(html, meta)
	htmxIdx := strings.Index(html, `src="/p-example/assets/vendor/htmx-2.0.10.min.js"`)
	if metaIdx == -1 {
		t.Fatalf("dashboard HTML missing htmx config meta:\n%s", html)
	}
	if htmxIdx == -1 || metaIdx > htmxIdx {
		t.Fatalf("htmx config meta must appear before htmx bundle:\n%s", html)
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

func TestCollectDashboardDataIncludesUsersAndTrafficSeries(t *testing.T) {
	s := newDashboardTestServer(t)
	now := time.Now().Unix()
	_, err := s.DB.Exec(`INSERT INTO users(label, secret_hex, enabled) VALUES('alice', '00112233445566778899aabbccddeeff', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(
		`INSERT INTO traffic_samples(user_label, ts, bytes_in, bytes_out, connections) VALUES(?,?,?,?,?)`,
		"alice", now-60, 20, 6, 2,
	)
	if err != nil {
		t.Fatal(err)
	}

	prev := dashboardHealthChecker
	dashboardHealthChecker = func() health.Checker {
		return health.Checker{
			Systemd: func(string) (bool, error) { return true, nil },
			HTTP:    func(string) error { return nil },
			SOCKS5:  func(string, string) (time.Duration, error) { return 0, nil },
		}
	}
	t.Cleanup(func() { dashboardHealthChecker = prev })

	data := s.collectDashboardData(metrics.Period1h)

	if data.IsBridge {
		t.Fatal("expected single mode")
	}
	if len(data.Users) != 1 || data.Users[0].Label != "alice" {
		t.Fatalf("users = %+v, want alice row", data.Users)
	}
	if len(data.TrafficSeries) != 60 {
		t.Fatalf("len(TrafficSeries) = %d, want 60", len(data.TrafficSeries))
	}
	if !data.Healthy || data.HealthLabel == "" {
		t.Fatalf("healthy summary = (%v, %q), want true and non-empty label", data.Healthy, data.HealthLabel)
	}

	var found bool
	for _, bucket := range data.TrafficSeries {
		if bucket.BytesIn == 20 && bucket.BytesOut == 6 && bucket.Connections == 2 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected traffic sample bucket in dashboard data")
	}
}

func TestCollectDashboardDataLoadsBridgeNodes(t *testing.T) {
	s := newDashboardTestServer(t)
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	writeNodeList(t, nodePath, bridge.NodeList{
		Nodes: []bridge.Node{
			{ID: 1, Type: bridge.NodeTypeTrojan, Tag: "de-1", Host: "de.example", Port: 443, Enabled: true, LastLatency: 42},
		},
		Strategy: "roundrobin",
	})
	s.BridgeCfg = &BridgeConfig{
		Paths: config.InstallPaths{
			OutboundsJSON: nodePath,
		},
	}

	prev := dashboardHealthChecker
	dashboardHealthChecker = func() health.Checker {
		return health.Checker{
			Systemd: func(string) (bool, error) { return true, nil },
			HTTP:    func(string) error { return nil },
			SOCKS5:  func(string, string) (time.Duration, error) { return 0, nil },
		}
	}
	t.Cleanup(func() { dashboardHealthChecker = prev })

	data := s.collectDashboardData(metrics.Period1h)

	if !data.IsBridge {
		t.Fatal("expected bridge mode")
	}
	if len(data.BridgeNodes) != 1 || data.BridgeNodes[0].Tag != "de-1" {
		t.Fatalf("bridge nodes = %+v, want de-1", data.BridgeNodes)
	}
	if !data.Healthy || data.HealthLabel == "" {
		t.Fatalf("healthy summary = (%v, %q), want true and non-empty label", data.Healthy, data.HealthLabel)
	}
}

func TestParseTeleproxyVersion(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "full usage banner",
			out: "usage: teleproxy [options] [relay-config]\n" +
				"       teleproxy generate-secret [domain]\n" +
				"teleproxy-4.15.0 compiled at Jan  1 2026 12:00:00 by gcc 13.2.0\n" +
				"\tMTProto proxy for Telegram\n",
			want: "4.15.0",
		},
		{
			name: "banner only",
			out:  "teleproxy-4.15.0 compiled at ...",
			want: "4.15.0",
		},
		{
			name: "version at end of line without trailing space",
			out:  "teleproxy-2.0.1",
			want: "2.0.1",
		},
		{
			name: "no banner",
			out:  "command not found",
			want: "",
		},
		{
			name: "empty",
			out:  "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseTeleproxyVersion(tt.out); got != tt.want {
				t.Errorf("parseTeleproxyVersion(%q) = %q, want %q", tt.out, got, tt.want)
			}
		})
	}
}

func TestParseSingboxVersion(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "multi-line version output",
			out:  "sing-box version 1.13.12\n\nEnvironment: go1.22 linux/amd64\nTags: with_quic\n",
			want: "1.13.12",
		},
		{
			name: "single line",
			out:  "sing-box version 1.13.12",
			want: "1.13.12",
		},
		{
			name: "bare version",
			out:  "1.13.12",
			want: "1.13.12",
		},
		{
			name: "empty",
			out:  "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseSingboxVersion(tt.out); got != tt.want {
				t.Errorf("parseSingboxVersion(%q) = %q, want %q", tt.out, got, tt.want)
			}
		})
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

// TestDashboardBridgeModeHidesStepTable verifies that in Bridge mode
// (IsBridge = true) the System card keeps its "System" heading (it reports the
// local host's resources, not the bridge chain) and does not render the
// per-step service table inside it (removed as redundant; the Services &
// Components panel is the canonical place for service status).
func TestDashboardBridgeModeHidesStepTable(t *testing.T) {
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

	// The System card title must stay "System" and must not be reframed as the
	// bridge chain health — the card body shows local host resources.
	if !strings.Contains(html, "<h3>System</h3>") {
		t.Error("Bridge mode dashboard should keep the 'System' card heading")
	}
	if strings.Contains(html, "Chain Health") {
		t.Error("Bridge mode dashboard should not reframe the System card as 'Chain Health'")
	}
	// The per-step status table must no longer be rendered in the System card.
	if strings.Contains(html, "socks5-inbound") {
		t.Error("Bridge mode dashboard should no longer render the per-step status table")
	}
	if strings.Contains(html, "telegram-chain") {
		t.Error("Bridge mode dashboard should no longer render the per-step status table")
	}
	// Services table heading must not appear (it belongs to Single mode only).
	if strings.Contains(html, "<h2>Services</h2>") {
		t.Error("Bridge mode dashboard should not contain Single-mode Services table")
	}
}

// TestDashboardSystemShowsAbsoluteTotals verifies that the Memory and Disk
// resource rows render the absolute total capacity next to the percentage
// (e.g. "67% / 16 GB"), so the percentage has a visible reference point.
func TestDashboardSystemShowsAbsoluteTotals(t *testing.T) {
	data := DashboardData{
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
		System: SystemSnapshot{
			MemoryPercent: 67,
			MemoryTotal:   16 * 1024 * 1024 * 1024, // 16 GiB
			DiskPercent:   42,
			DiskTotal:     100 * 1024 * 1024 * 1024, // 100 GiB
		},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	if !strings.Contains(html, "67% / 16 GB") {
		t.Error("Memory row should show percentage and absolute total, e.g. '67% / 16 GB'")
	}
	if !strings.Contains(html, "42% / 100 GB") {
		t.Error("Disk row should show percentage and absolute total, e.g. '42% / 100 GB'")
	}
}

// TestDashboardSystemOmitsTotalWhenUnknown verifies that when the host stats are
// unavailable (totals are zero) the rows fall back to the plain percentage label
// without a trailing slash.
func TestDashboardSystemOmitsTotalWhenUnknown(t *testing.T) {
	data := DashboardData{
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
		System: SystemSnapshot{
			MemoryPercent: -1,
			DiskPercent:   -1,
		},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	if strings.Contains(html, "unknown / ") {
		t.Error("rows should not render a trailing slash when totals are unknown")
	}
}

// TestDashboardLiveConnectionsSection verifies that the Active Connections
// section is rendered when LiveConnections data is present.
func TestDashboardLiveConnectionsSection(t *testing.T) {
	data := DashboardData{
		LiveConnections: []metrics.Sample{
			{UserLabel: "alice", BytesIn: 1024, BytesOut: 2048, Connections: 3},
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
	if !strings.Contains(html, "1.0 KB") || !strings.Contains(html, "2.0 KB") {
		t.Fatalf("dashboard should show live upload and download bytes:\n%s", html)
	}
	if !strings.Contains(html, "bob") {
		t.Error("dashboard should list user 'bob' in Active Connections")
	}
}

func TestDashboardRendersTeleproxyOperationalMetrics(t *testing.T) {
	data := DashboardData{
		PanelPath: "/p-example/",
		Period:    metrics.Period24h,
		Teleproxy: metrics.Snapshot{
			AcceptedConnectionsTotal: 120,
			RejectedConnectionsTotal: 7,
			SOCKS5: metrics.UpstreamCounters{
				Attempted: 20,
				Succeeded: 18,
				Failed:    2,
			},
			JA4: []metrics.JA4Counter{
				{Hash: "t13d1615h2_46e7e9700bed_45f260be83e2", Count: 8},
			},
		},
		LiveConnections: []metrics.Sample{
			{
				UserLabel:           "alice",
				Connections:         4,
				ConnectionLimit:     10,
				UniqueIPs:           2,
				MaxIPs:              5,
				RejectedConnections: 3,
				RejectedQuota:       1,
				RejectedIPs:         1,
				RejectedExpired:     1,
			},
		},
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	for _, want := range []string{
		"Connection quality",
		"Accepted",
		"Rejected",
		"120",
		"7",
		"SOCKS5 upstream",
		"90%",
		"Security signals",
		"t13d1615h2_46e7e9700bed_45f260be83e2",
		"Limits",
		"4 / 10",
		"2 / 5 IPs",
		"quota 1",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard operational metrics missing %q:\n%s", want, html)
		}
	}
}

// TestDashboardNoLiveConnectionsShowsEmptyState verifies that the Active
// Connections section remains visible and renders the empty state when no live
// data is available.
func TestDashboardNoLiveConnectionsShowsEmptyState(t *testing.T) {
	data := DashboardData{
		LiveConnections: nil,
		Components:      []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	if !strings.Contains(html, "Active Connections") {
		t.Error("dashboard should contain 'Active Connections' heading when no live data")
	}
	if !strings.Contains(html, "No active connections.") {
		t.Error("dashboard should render empty connections state when no live data")
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

func TestDashboardTopUsersPeriodSelectorReflectsSelectedPeriod(t *testing.T) {
	data := DashboardData{
		PanelPath: "/p-example/",
		Period:    "7d",
		TopUsers: []metrics.UserTraffic{
			{UserLabel: "alice", BytesIn: 1024, BytesOut: 2048, Connections: 1},
		},
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	if !strings.Contains(html, `class="seg-item active" href="?period=7d"`) {
		t.Errorf("selected period 7d should be marked active:\n%s", html)
	}
	if strings.Contains(html, `class="seg-item active" href="?period=24h"`) {
		t.Errorf("24h must not be active when 7d is selected:\n%s", html)
	}
}

func TestDashboardTrafficFragmentRendersSeriesOverview(t *testing.T) {
	var buf bytes.Buffer
	dashboardTrafficFragment(&buf, DashboardData{
		PanelPath: "/p-example/",
		Period:    "24h",
		TrafficSeries: []metrics.TrafficBucket{
			{TS: 1, BytesIn: 1024, BytesOut: 512, Connections: 2},
			{TS: 2, BytesIn: 2048, BytesOut: 1024, Connections: 5},
		},
	})

	html := buf.String()
	for _, want := range []string{
		`class="card-body traffic-overview"`,
		`class="traffic-chart area-chart"`,
		`traffic-chart-line-out`,
		`traffic-chart-line-in`,
		`3.0 KB`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("traffic fragment missing %q:\n%s", want, html)
		}
	}
}

func TestDashboardPageRendersKPIAndQuickActions(t *testing.T) {
	data := DashboardData{
		PanelPath:   "/p-example/",
		Period:      "24h",
		Healthy:     true,
		HealthLabel: "All systems operational",
		Users:       []UserRow{{Label: "alice"}, {Label: "bob"}},
		LiveConnections: []metrics.Sample{
			{UserLabel: "alice", Connections: 2},
		},
		TrafficSeries: []metrics.TrafficBucket{
			{TS: 1, BytesIn: 1024, BytesOut: 512},
		},
		BridgeNodes: []bridge.Node{
			{Tag: "de-1", Type: bridge.NodeTypeTrojan, Host: "de.example", Enabled: true, LastLatency: 42},
		},
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	for _, want := range []string{
		`class="kpi-grid"`,
		`Throughput`,
		`Active users`,
		`Quick actions`,
		`href="/p-example/logs"`,
		`data-action="users"`,
		`data-action="bridge"`,
		`Bridge nodes`,
		`ready`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard page missing %q:\n%s", want, html)
		}
	}
}

func TestDashboardBridgeSummaryLimitsPreviewAndLinksToBridge(t *testing.T) {
	data := DashboardData{
		PanelPath: "/p-example/",
		Period:    "24h",
		BridgeNodes: []bridge.Node{
			{Tag: "de-1", Type: bridge.NodeTypeTrojan, Host: "de.example", Enabled: true, LastLatency: 42},
			{Tag: "nl-1", Type: bridge.NodeTypeTrojan, Host: "nl.example", Enabled: true, LastLatency: 51},
			{Tag: "fr-1", Type: bridge.NodeTypeTrojan, Host: "fr.example", Enabled: true, LastLatency: 66},
			{Tag: "us-1", Type: bridge.NodeTypeTrojan, Host: "us.example", Enabled: true, LastLatency: 73},
		},
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	}

	var buf bytes.Buffer
	dashboardPage(&buf, data)
	html := buf.String()

	for _, want := range []string{
		`href="/p-example/bridge"`,
		`Open Bridge`,
		`de-1`,
		`nl-1`,
		`fr-1`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard bridge summary missing %q:\n%s", want, html)
		}
	}
	if !strings.Contains(html, `us-1`) {
		t.Fatalf("dashboard bridge summary must match the template four-node preview:\n%s", html)
	}
}

func TestDashboardHealthFragmentIncludesSystemPanel(t *testing.T) {
	var buf bytes.Buffer
	dashboardHealthFragment(&buf, DashboardData{
		PanelPath:   "/p-example/",
		Period:      "24h",
		HealthLabel: "All systems operational",
		System:      SystemSnapshot{MemoryPercent: 62, DiskPercent: 41, LoadAvg: "0.42", Uptime: "3d 4h", Kernel: "6.8.0"},
		Users:       []UserRow{{Label: "alice", Enabled: true}, {Label: "bob", Enabled: false}},
		BridgeNodes: []bridge.Node{{Tag: "de-1", Enabled: true}},
		Services:    []health.ServiceState{{Name: "teleproxy.service", Active: true, Message: "running"}},
		IsBridge:    false,
	})

	html := buf.String()
	for _, want := range []string{
		`class="resource-row"`,
		`class="resource-meta"`,
		`class="bar"`,
		`Memory`,
		`Disk`,
		`Load avg`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("health fragment missing %q:\n%s", want, html)
		}
	}
}

func TestDashboardConnectionsFragmentIncludesSummaryMetrics(t *testing.T) {
	var buf bytes.Buffer
	dashboardConnectionsFragment(&buf, DashboardData{
		PanelPath: "/p-example/",
		Period:    "24h",
		Users:     []UserRow{{Label: "alice"}, {Label: "bob"}, {Label: "carol"}},
		LiveConnections: []metrics.Sample{
			{UserLabel: "alice", Connections: 3},
			{UserLabel: "bob", Connections: 1},
		},
	})

	html := buf.String()
	for _, want := range []string{
		`class="card-head"`,
		`Active Connections`,
		`Connections`,
		`4 live`,
		`alice`,
		`bob`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("connections fragment missing %q:\n%s", want, html)
		}
	}
}

func TestDashboardTrafficFragmentRendersDualSeriesChartAndLegend(t *testing.T) {
	var buf bytes.Buffer
	dashboardTrafficFragment(&buf, DashboardData{
		PanelPath: "/p-example/",
		Period:    "24h",
		TrafficSeries: []metrics.TrafficBucket{
			{TS: 1, BytesIn: 1024, BytesOut: 512},
			{TS: 2, BytesIn: 2048, BytesOut: 1024},
		},
	})

	html := buf.String()
	for _, want := range []string{
		`class="card-head"`,
		`href="?period=1h"`,
		`class="seg-item active" href="?period=24h"`,
		`href="?period=7d"`,
		`href="?period=30d"`,
		`class="legend-dot"`,
		`traffic-chart-area traffic-chart-area-out`,
		`traffic-chart-line traffic-chart-line-out`,
		`traffic-chart-area traffic-chart-area-in`,
		`traffic-chart-line traffic-chart-line-in`,
		`traffic-chart-line traffic-chart-line-connections`,
		`Download`,
		`Upload`,
		`Connections`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("traffic fragment missing %q:\n%s", want, html)
		}
	}
}

func TestDashboardComponentsFragmentIncludesComponentStatusGrid(t *testing.T) {
	var buf bytes.Buffer
	dashboardComponentsFragment(&buf, DashboardData{
		PanelPath: "/p-example/",
		Period:    "24h",
		IsBridge:  false,
		System:    SystemSnapshot{Kernel: "6.8.0", LoadAvg: "0.42"},
		BridgeNodes: []bridge.Node{
			{Tag: "de-1", Enabled: true},
		},
		Components: []ComponentVersion{
			{Name: "tgproxy-panel", Version: "v0.0.0-test"},
			{Name: "sing-box", Version: "unknown"},
		},
	})

	html := buf.String()
	for _, want := range []string{
		`class="tbl tbl--compact"`,
		`Services & Components`,
		`systemd units and installed binaries`,
		`Optional in Single mode`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("components fragment missing %q:\n%s", want, html)
		}
	}
}

func TestDashboardHealthFragmentRendersSingleMode(t *testing.T) {
	data := DashboardData{
		PanelPath: "/p-example/",
		Period:    "24h",
		IsBridge:  false,
		Services:  []health.ServiceState{{Name: "teleproxy.service", Active: true, Message: "running"}},
	}

	var buf bytes.Buffer
	dashboardHealthFragment(&buf, data)
	html := buf.String()

	if !strings.Contains(html, `id="dashboard-health"`) {
		t.Fatalf("health fragment missing stable target id:\n%s", html)
	}
	// Services are no longer rendered in the health fragment for Single mode;
	// they are displayed in the "Services & Components" panel instead.
	if strings.Contains(html, "sing-box.service") {
		t.Fatalf("single-mode health fragment must not require sing-box:\n%s", html)
	}
	assertDashboardFragmentHTMX(t, html, "health", "dashboard-health")
}

func TestDashboardHealthFragmentRendersBridgeMode(t *testing.T) {
	data := DashboardData{
		IsBridge: true,
		BridgeSteps: []health.BridgeStepStatus{
			{Name: "teleproxy.service", OK: true, Message: "running"},
			{Name: "sing-box.service", OK: true, Message: "running"},
		},
	}

	var buf bytes.Buffer
	dashboardHealthFragment(&buf, data)
	html := buf.String()

	if !strings.Contains(html, "Bridge Mode") {
		t.Fatalf("bridge health fragment missing bridge state:\n%s", html)
	}
	// The per-step service table was removed from the System card; service
	// status now lives only in the Services & Components panel.
	if strings.Contains(html, "sing-box.service") {
		t.Fatalf("bridge health fragment must no longer render the per-step table:\n%s", html)
	}
}

func TestDashboardConnectionsFragmentRendersEmptyState(t *testing.T) {
	var buf bytes.Buffer
	dashboardConnectionsFragment(&buf, DashboardData{PanelPath: "/p-example/", Period: "24h"})
	html := buf.String()

	if !strings.Contains(html, `id="dashboard-connections"`) || !strings.Contains(html, "No active connections") {
		t.Fatalf("connections fragment missing empty state:\n%s", html)
	}
	assertDashboardFragmentHTMX(t, html, "connections", "dashboard-connections")
}

func TestDashboardFragmentsDoNotExposeSensitiveTokens(t *testing.T) {
	data := DashboardData{
		PanelPath: "/p-example/",
		Period:    "24h",
		LiveConnections: []metrics.Sample{
			{UserLabel: "alice", Connections: 2},
		},
	}

	var buf bytes.Buffer
	dashboardConnectionsFragment(&buf, data)

	assertNoSensitiveDashboardTokens(t, buf.String())
}

func TestDashboardTrafficFragmentPreservesHTMXWrapper(t *testing.T) {
	var buf bytes.Buffer
	dashboardTrafficFragment(&buf, DashboardData{PanelPath: "/p-example/", Period: "24h"})
	assertDashboardFragmentHTMX(t, buf.String(), "traffic", "dashboard-traffic")
}

func TestDashboardComponentsFragmentPreservesHTMXWrapper(t *testing.T) {
	var buf bytes.Buffer
	dashboardComponentsFragment(&buf, DashboardData{
		PanelPath:  "/p-example/",
		Period:     "24h",
		Components: []ComponentVersion{{Name: "tgproxy-panel", Version: "v0.0.0-test"}},
	})
	assertDashboardFragmentHTMX(t, buf.String(), "components", "dashboard-components")
}

func assertDashboardFragmentHTMX(t *testing.T, html, name, event string) {
	t.Helper()

	for _, want := range []string{
		`id="` + event + `"`,
		`hx-get="/p-example/dashboard/fragments/` + name + `?period=24h"`,
		`hx-trigger="sse:` + event + `"`,
		`hx-swap="outerHTML"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("%s fragment missing %q:\n%s", name, want, html)
		}
	}
}

func TestDashboardFragmentRequiresAuth(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/fragments/health", nil)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauthenticated fragment status = %d, want 303", rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "/p-example/login" {
		t.Fatalf("Location = %q, want %q", location, "/p-example/login")
	}
}

func TestDashboardFragmentUsesNoStore(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/fragments/connections", nil)
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("fragment status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want %q", got, "no-store")
	}
	if csp := rec.Header().Get("Content-Security-Policy"); strings.Contains(csp, "'unsafe-inline'") {
		t.Fatalf("fragment CSP = %q, must not allow unsafe-inline", csp)
	}
	if !strings.Contains(rec.Body.String(), `id="dashboard-connections"`) {
		t.Fatalf("connections fragment missing target id:\n%s", rec.Body.String())
	}
}

func TestDashboardFragmentUnknownReturnsNotFound(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/fragments/unknown", nil)
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown fragment status = %d, want 404", rec.Code)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); strings.Contains(csp, "'unsafe-inline'") {
		t.Fatalf("fragment CSP = %q, must not allow unsafe-inline", csp)
	}
}

func assertNoSensitiveDashboardTokens(t *testing.T, body string) {
	t.Helper()

	normalized := strings.ToLower(body)
	for _, forbidden := range []string{"session_id", "csrf_token", "_csrf", "password", "secret"} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("dashboard output contains forbidden token %q:\n%s", forbidden, body)
		}
	}
}
