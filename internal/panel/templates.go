package panel

import (
	"html/template"
	"io"
)

var loginTmpl = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Login</title>
<link rel="stylesheet" href="{{.PanelPath}}assets/panel.css">
</head>
<body class="login-page">
<div class="app login-app">
<main class="login-shell">
<div class="card login-card">
<p class="page-eyebrow">MTProto Orchestrator</p>
<h1>Admin Login</h1>
<p class="page-sub">Authenticate to manage users, Bridge nodes, logs, and TLS settings.</p>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Login</label><input type="text" name="login" autocomplete="username" required>
<label>Password</label><input type="password" name="password" autocomplete="current-password" required>
<button type="submit">Sign in</button>
</form>
</div>
</main>
</div>
</body>
</html>
`))

var dashboardTmpl = template.Must(template.New("dashboard").Funcs(dashboardFragmentFuncs).Parse(dashboardFragments + `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Dashboard</title>
<link rel="stylesheet" href="{{.PanelPath}}assets/panel.css">
<meta name="htmx-config" content='{"includeIndicatorStyles":false}'>
<script defer src="{{.PanelPath}}assets/vendor/htmx-2.0.10.min.js"></script>
<script defer src="{{.PanelPath}}assets/vendor/htmx-ext-sse-2.2.4.js"></script>
<script defer src="{{.PanelPath}}assets/panel.js"></script>
</head>
<body>
<div class="app">
` + sharedTopbar + `
<main class="page dashboard-page" hx-ext="sse" sse-connect="{{.PanelPath}}dashboard/events?period={{.Period}}" sse-close="dashboard-close">
<section class="page-head ops-page-head">
  <div class="titles">
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Control Dashboard</h1>
    <p class="page-sub">{{if .IsBridge}}Bridge routing is active. Watch each hop in the chain.{{else}}Single mode is active. Core service health is shown first.{{end}}</p>
  </div>
  <div class="actions">
    <div class="status-strip" aria-label="Dashboard status">
      <div class="item">
        <span class="k">Mode</span>
        <span class="v">{{if .IsBridge}}Bridge{{else}}Single{{end}}</span>
      </div>
      <div class="item">
        <span class="k">Uptime</span>
        <span class="v mono">{{.System.Uptime}}</span>
      </div>
      <div class="item">
        <span class="k">Heartbeat</span>
        <span class="v mono ops-heartbeat" sse-swap="dashboard-heartbeat">connecting</span>
      </div>
    </div>
  </div>
</section>
<section class="kpi-grid" aria-label="Dashboard overview">
  <article class="kpi">
    <div class="kpi-label">Throughput</div>
    <div class="kpi-value mono">{{formatBytes (latestTrafficTotal .TrafficSeries)}}</div>
    <div class="kpi-sub">
      <span class="kpi-delta kpi-delta--up">latest bucket</span>
      <span>selected {{.Period}} window</span>
    </div>
  </article>
  <article class="kpi">
    <div class="kpi-label">Active users</div>
    <div class="kpi-value mono">{{len .LiveConnections}}</div>
    <div class="kpi-sub">
      <span>{{len .Users}} configured</span>
      <span>{{sumLiveConnections .LiveConnections}} connections</span>
    </div>
  </article>
  <article class="kpi">
    <div class="kpi-label">Bridge nodes</div>
    <div class="kpi-value mono">{{countEnabledNodes .BridgeNodes}}/{{len .BridgeNodes}}</div>
    <div class="kpi-sub">
      {{if .IsBridge}}<span>enabled for routing</span>{{else}}<span>optional in Single mode</span>{{end}}
    </div>
  </article>
  <article class="kpi">
    <div class="kpi-label">Service health</div>
    <div class="kpi-value">{{if .Healthy}}All systems{{else}}Needs attention{{end}}</div>
    <div class="kpi-sub">
      <span>{{.HealthLabel}}</span>
    </div>
  </article>
</section>
<section class="page-stack">
  {{template "health" .}}
  <div class="grid-12">
    <div class="col-8">
      {{template "components" .}}
    </div>
    <div class="col-4">
      <section class="card ops-card">
        <h2>Quick Actions</h2>
        <p class="ops-card-sub">Primary operator flows available in the current panel.</p>
        <div class="action-list">
          <a class="action-link" href="{{.PanelPath}}users">
            <span class="action-icon" data-action="users" aria-hidden="true"></span>
            <span class="summary-copy"><strong>Add user</strong><span>Create a new MTProto access link.</span></span>
          </a>
          <a class="action-link" href="{{.PanelPath}}bridge">
            <span class="action-icon" data-action="bridge" aria-hidden="true"></span>
            <span class="summary-copy"><strong>Manage Bridge</strong><span>Update outbound nodes and routing strategy.</span></span>
          </a>
          <a class="action-link" href="{{.PanelPath}}settings/certificates">
            <span class="action-icon" data-action="tls" aria-hidden="true"></span>
            <span class="summary-copy"><strong>Review TLS</strong><span>Check certificate validity and renewal status.</span></span>
          </a>
          <a class="action-link" href="{{.PanelPath}}logs">
            <span class="action-icon" data-action="logs" aria-hidden="true"></span>
            <span class="summary-copy"><strong>Open logs</strong><span>Inspect the live event stream and download recent lines.</span></span>
          </a>
        </div>
      </section>
    </div>
  </div>
  <div class="grid-12">
    <div class="col-8">
      {{template "traffic" .}}
    </div>
    <div class="col-4">
      <div class="page-stack">
        {{template "connections" .}}
        <section class="card ops-card">
          <h2>Bridge Nodes</h2>
          <p class="ops-card-sub">{{countEnabledNodes .BridgeNodes}} of {{len .BridgeNodes}} nodes enabled.</p>
          {{if .BridgeNodes}}
          <div class="summary-list">
            {{range previewBridgeNodes .BridgeNodes 3}}
            <div class="summary-row">
              <span class="badge {{bridgeNodeStateClass .}}">{{bridgeNodeStateLabel .}}</span>
              <span class="summary-copy">
                <strong>{{.Tag}}</strong>
                <span class="mono">{{.Type}} · {{.Host}}{{if .LastLatency}} · {{.LastLatency}}ms{{end}}</span>
              </span>
            </div>
            {{end}}
          </div>
          <nav class="page-nav" aria-label="Bridge summary links">
            <a href="{{.PanelPath}}bridge">Open Bridge</a>
          </nav>
          {{else}}
          <p class="empty">No Bridge nodes configured.</p>
          {{end}}
        </section>
      </div>
    </div>
  </div>
</section>
</main>
</div>
</body>
</html>
`))

type loginPageData struct {
	PanelPath string
	CSRFField string
	CSRFToken string
	Error     string
}

func loginPage(w io.Writer, panelPath, csrfToken, errMsg string) {
	loginTmpl.Execute(w, loginPageData{ //nolint:errcheck
		PanelPath: panelPath,
		CSRFField: CSRFField(),
		CSRFToken: csrfToken,
		Error:     errMsg,
	})
}

func dashboardPage(w io.Writer, data DashboardData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "dashboard"
	}
	dashboardTmpl.Execute(w, data) //nolint:errcheck
}
