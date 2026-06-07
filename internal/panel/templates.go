package panel

import (
	"html/template"
	"io"
)

var loginTmpl = template.Must(template.New("login").Funcs(template.FuncMap{"assetv": assetVersion}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Login</title>
<link rel="preload" href="{{.PanelPath}}assets/fonts/geist/Geist-Regular.woff2" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="{{.PanelPath}}assets/fonts/geist/Geist-Medium.woff2" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="{{.PanelPath}}assets/panel.css{{assetv "panel.css"}}">
</head>
<body class="login-page">
<div class="app login-app">
<main class="login-shell">
<div class="card login-card">
<p class="page-eyebrow">MTProto Orchestrator</p>
<h1>Admin Login</h1>
<p class="page-sub">Authenticate to manage users, Bridge nodes, logs, and TLS settings.</p>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" class="stack-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label class="label">Login</label><input class="input input--mono" type="text" name="login" autocomplete="username" required>
<label class="label">Password</label><input class="input input--mono" type="password" name="password" autocomplete="current-password" required>
<button class="btn" data-variant="primary" type="submit">Sign in</button>
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
<link rel="preload" href="{{.PanelPath}}assets/fonts/geist/Geist-Regular.woff2" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="{{.PanelPath}}assets/fonts/geist/Geist-Medium.woff2" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="{{.PanelPath}}assets/panel.css{{assetv "panel.css"}}">
<meta name="htmx-config" content='{"includeIndicatorStyles":false}'>
<script defer src="{{.PanelPath}}assets/vendor/htmx-2.0.10.min.js"></script>
<script defer src="{{.PanelPath}}assets/vendor/htmx-ext-sse-2.2.4.js"></script>
<script defer src="{{.PanelPath}}assets/panel.js{{assetv "panel.js"}}"></script>
</head>
<body>
<div class="app">
` + sharedTopbar + `
<main class="page dashboard-page" hx-ext="sse" sse-connect="{{.PanelPath}}dashboard/events?period={{.Period}}" sse-close="dashboard-close">
<section class="page-head ops-page-head">
  <div class="titles">
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Control</h1>
    <p class="page-sub">Overview of services, traffic and users.</p>
  </div>
  <div class="actions">
    <div class="status-strip" aria-label="Dashboard status">
      <div class="item">
        <span class="pulse-dot"></span>
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
    <div class="kpi-label">{{icon "Activity" 13}} Throughput</div>
    <div class="kpi-value">{{formatBytes (latestTrafficTotal .TrafficSeries)}}</div>
    <div class="kpi-sub">
      <span class="kpi-delta kpi-delta--up">{{icon "ArrowUpRight" 12}} live</span>
      <span>selected {{.Period}} window</span>
    </div>
    {{kpiSparkSVG .TrafficSeries}}
  </article>
  <article class="kpi">
    <div class="kpi-label">{{icon "Users" 13}} Active users</div>
    <div class="kpi-value">{{len .LiveConnections}}</div>
    <div class="kpi-sub">
      <span>{{len .Users}} configured</span>
      <span>{{sumLiveConnections .LiveConnections}} connections</span>
    </div>
  </article>
  <article class="kpi">
    <div class="kpi-label">{{icon "Bridge" 13}} Bridge nodes</div>
    <div class="kpi-value">{{countEnabledNodes .BridgeNodes}}/{{len .BridgeNodes}}</div>
    <div class="kpi-sub">
      {{if .IsBridge}}<span>enabled for routing</span>{{else}}<span>optional in Single mode</span>{{end}}
    </div>
  </article>
  <article class="kpi">
    <div class="kpi-label">{{icon "Heart" 13}} Service health</div>
    <div class="kpi-value">{{if .Healthy}}All systems{{else}}Needs attention{{end}}</div>
    <div class="kpi-sub">
      <span><span class="pulse-dot pulse-dot--inline"></span>{{.HealthLabel}}</span>
    </div>
  </article>
</section>
<section class="page-stack fade-in">
  <div class="dash-cols">
    <div class="dash-main">
      {{template "traffic" .}}
      {{template "components" .}}
      {{template "top_users" .}}
      {{template "connections" .}}
    </div>
    <div class="dash-rail">
      {{template "health" .}}
      {{template "upstream" .}}
      {{template "ops" .}}
      {{template "bridge_nodes" .}}
    </div>
  </div>
  <section class="card quick-actions">
    <div class="card-head"><h3>Quick actions</h3></div>
    <div class="card-body">
      <div class="quick-actions-bar">
        <a class="action-row" data-action="users" href="{{.PanelPath}}users#create-user">
              <span class="action-icon">{{icon "Plus" 14}}</span>
              <span class="summary-copy"><strong>Add user</strong><span>Create a new MTProto user and copy the link</span></span>
              {{icon "Right" 14}}
            </a>
            <form method="post" action="{{.PanelPath}}users/rotate-all" class="action-row-form" onsubmit="return confirm('Rotate secrets for all enabled users? Existing share links stop working until you redistribute the new ones.')">
              <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}" class="js-csrf">
              <button class="action-row" type="submit">
                <span class="action-icon">{{icon "Key" 14}}</span>
                <span class="summary-copy"><strong>Rotate all secrets</strong><span>Re-issue secrets for all enabled users</span></span>
                {{icon "Right" 14}}
              </button>
            </form>
            <a class="action-row" data-action="bridge" href="{{.PanelPath}}bridge">
              <span class="action-icon">{{icon "Cloud" 14}}</span>
              <span class="summary-copy"><strong>Add outbound node</strong><span>VLESS / Trojan / SS / Hysteria2 / TUIC</span></span>
              {{icon "Right" 14}}
            </a>
            <a class="action-row" data-action="tls" href="{{.PanelPath}}settings/certificates">
              <span class="action-icon">{{icon "Cert" 14}}</span>
              <span class="summary-copy"><strong>Renew certificate</strong><span>Check certificate validity and renewal status</span></span>
              {{icon "Right" 14}}
            </a>
            <a class="action-row" data-action="logs" href="{{.PanelPath}}settings/stubs">
              <span class="action-icon">{{icon "Stubs" 14}}</span>
              <span class="summary-copy"><strong>Change camouflage</strong><span>Pick a stub template</span></span>
              {{icon "Right" 14}}
            </a>
      </div>
    </div>
  </section>
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
