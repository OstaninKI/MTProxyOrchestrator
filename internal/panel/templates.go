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
<div class="card login-card">
<h1>Admin Login</h1>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Login</label><input type="text" name="login" autocomplete="username" required>
<label>Password</label><input type="password" name="password" autocomplete="current-password" required>
<button type="submit">Sign in</button>
</form>
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
<body class="dashboard-page">
<main class="shell ops-shell" hx-ext="sse" sse-connect="{{.PanelPath}}dashboard/events?period={{.Period}}" sse-close="dashboard-close">
<header class="topbar ops-topbar">
  <div class="ops-heading">
    <p class="eyebrow">MTProto Orchestrator</p>
    <h1>Control Dashboard</h1>
    <p class="sub">{{if .IsBridge}}Bridge routing is active. Watch each hop in the chain.{{else}}Single mode is active. Core service health is shown first.{{end}}</p>
    <div class="ops-statusbar" aria-label="Dashboard status">
      <span class="ops-chip {{if .IsBridge}}ops-chip-warn{{else}}ops-chip-ok{{end}}">{{if .IsBridge}}Bridge mode{{else}}Single mode{{end}}</span>
      <span class="ops-chip ops-chip-neutral">SSE heartbeat <span class="ops-heartbeat" sse-swap="dashboard-heartbeat">connecting</span></span>
      <span class="ops-chip ops-chip-neutral">Period {{.Period}}</span>
    </div>
  </div>
  <nav class="nav" aria-label="Primary">
    <a href="{{.PanelPath}}users">Users</a>
    <a href="{{.PanelPath}}bridge">Bridge</a>
    <a href="{{.PanelPath}}logs">Logs</a>
    <a href="{{.PanelPath}}settings/stubs">Stubs</a>
    <a href="{{.PanelPath}}settings/certificates">Certificates</a>
    <a href="{{.PanelPath}}settings/proxy">Proxy</a>
    <a href="{{.PanelPath}}settings/admin-password">Password</a>
    <a href="{{.PanelPath}}settings/system">System</a>
    <form method="post" action="{{.PanelPath}}logout" class="inline"><input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}" class="js-csrf"><button type="submit" class="logout">Logout</button></form>
  </nav>
</header>
<section class="grid ops-grid">
{{template "health" .}}
{{template "components" .}}
{{template "connections" .}}

{{template "traffic" .}}
</section>
</main>
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
	dashboardTmpl.Execute(w, data) //nolint:errcheck
}
