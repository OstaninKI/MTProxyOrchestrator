package panel

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogsPageUsesLocalShellAssets(t *testing.T) {
	var buf bytes.Buffer

	logsPage(&buf, logsPageData{PanelPath: "/p-example", CSRFToken: "test-token"})
	html := buf.String()

	for _, want := range []string{
		`href="/p-example/assets/panel.css"`,
		`src="/p-example/assets/panel.js"`,
		`class="app"`,
		`class="topbar"`,
		`class="page-head"`,
		`class="summary-grid logs-kpi-grid"`,
		`class="card-body logs-toolbar"`,
		`class="logs-view"`,
		`class="card-footer logs-footer"`,
		`data-logs-role="summary"`,
		`data-logs-role="buffer-chip"`,
		`data-logs-page`,
		`data-panel-path="/p-example"`,
		`data-logs-role="count-buffered"`,
		`data-logs-role="count-info"`,
		`data-logs-role="count-warn"`,
		`data-logs-role="count-error"`,
		`data-logs-role="container"`,
		`data-logs-role="status"`,
		`data-logs-role="component"`,
		`data-logs-role="level"`,
		`data-logs-role="level-buttons"`,
		`data-logs-role="search"`,
		`data-logs-role="pause"`,
		`data-logs-role="clear"`,
		`data-logs-role="autoscroll"`,
		`data-logs-role="download"`,
		`class="logs-level active" data-level="info"`,
		`class="nav-item" data-active="true" href="/p-example/logs"`,
		`action="/p-example/logout"`,
		`href="/p-example/dashboard"`,
		`href="/p-example/settings/system"`,
		`href="/p-example/logs"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("logs page missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<style>") {
		t.Fatalf("logs page must not render inline style:\n%s", html)
	}
	if strings.Contains(html, "new WebSocket(") || strings.Contains(html, "buildWsURL") {
		t.Fatalf("logs page must not render inline logs javascript:\n%s", html)
	}
}
