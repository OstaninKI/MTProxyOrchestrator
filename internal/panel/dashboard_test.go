package panel

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
		`href="/p-example/assets/panel.css"`,
		`<meta name="htmx-config" content='{"includeIndicatorStyles":false}'>`,
		`src="/p-example/assets/vendor/htmx-2.0.10.min.js"`,
		`src="/p-example/assets/vendor/htmx-ext-sse-2.2.4.js"`,
		`src="/p-example/assets/panel.js"`,
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
		`hx-trigger="sse:dashboard-health"`,
		`hx-trigger="sse:dashboard-connections"`,
		`hx-trigger="sse:dashboard-traffic"`,
		`hx-trigger="sse:dashboard-components"`,
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
		`href="/p-example/assets/panel.css"`,
		`src="/p-example/assets/vendor/htmx-2.0.10.min.js"`,
		`src="/p-example/assets/vendor/htmx-ext-sse-2.2.4.js"`,
		`src="/p-example/assets/panel.js"`,
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
	if !strings.Contains(html, "teleproxy.service") || !strings.Contains(html, "running") {
		t.Fatalf("health fragment missing service data:\n%s", html)
	}
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

	if !strings.Contains(html, "Bridge Mode") || !strings.Contains(html, "sing-box.service") {
		t.Fatalf("bridge health fragment missing bridge state:\n%s", html)
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
