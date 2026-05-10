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
<style>body{font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f5f5}
.card{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,.12);width:320px}
h1{margin:0 0 1.5rem;font-size:1.4rem;color:#333}
label{display:block;margin-bottom:.25rem;font-size:.875rem;color:#555}
input{width:100%;box-sizing:border-box;padding:.5rem;border:1px solid #ccc;border-radius:4px;margin-bottom:1rem;font-size:1rem}
button{width:100%;padding:.6rem;background:#2563eb;color:#fff;border:none;border-radius:4px;font-size:1rem;cursor:pointer}
button:hover{background:#1d4ed8}.error{color:#dc2626;margin-bottom:1rem;font-size:.875rem}</style>
</head>
<body>
<div class="card">
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

var dashboardTmpl = template.Must(template.New("dashboard").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Dashboard</title>
<style>
:root{--bg:#09090b;--card:#101014;--card2:#15151b;--border:#27272f;--text:#f4f4f5;--muted:#a1a1aa;--accent:#38bdf8;--good:#22c55e;--bad:#f43f5e}
*{box-sizing:border-box}body{margin:0;min-height:100vh;background:radial-gradient(circle at 18% -8%,rgba(56,189,248,.2),transparent 34%),linear-gradient(135deg,#09090b,#111116 48%,#0c0f14);color:var(--text);font-family:Aptos,"Segoe UI",sans-serif}
body:before{content:"";position:fixed;inset:0;pointer-events:none;background-image:linear-gradient(rgba(255,255,255,.035) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.028) 1px,transparent 1px);background-size:44px 44px;mask-image:linear-gradient(to bottom,rgba(0,0,0,.95),transparent 82%)}
a{color:inherit}.shell{position:relative;max-width:1240px;margin:0 auto;padding:28px}.topbar{display:flex;justify-content:space-between;gap:18px;align-items:flex-start;margin-bottom:22px}.eyebrow{color:var(--accent);font-size:.72rem;text-transform:uppercase;letter-spacing:.14em;font-weight:700;margin:0 0 8px}h1{font-size:2.15rem;line-height:1;margin:0;letter-spacing:0}h2{font-size:1rem;margin:0 0 14px}.sub{color:var(--muted);margin:.65rem 0 0}.nav{display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end}.nav a,.logout{display:inline-flex;align-items:center;height:34px;padding:0 12px;border-radius:7px;border:1px solid var(--border);background:rgba(255,255,255,.04);color:var(--text);text-decoration:none;font-size:.86rem;cursor:pointer}.nav a:hover,.logout:hover{border-color:rgba(56,189,248,.55);background:rgba(56,189,248,.1)}.logout{font:inherit}.grid{display:grid;grid-template-columns:1.15fr .85fr;gap:16px}.card{background:linear-gradient(180deg,var(--card),var(--card2));border:1px solid var(--border);border-radius:8px;padding:18px;box-shadow:0 18px 50px rgba(0,0,0,.28)}.span2{grid-column:1/-1}.table-wrap{overflow:auto;border:1px solid var(--border);border-radius:8px}table{width:100%;border-collapse:collapse;min-width:520px}th,td{text-align:left;padding:12px 14px;border-bottom:1px solid var(--border);font-size:.9rem}th{color:var(--muted);font-size:.74rem;text-transform:uppercase;letter-spacing:.08em;background:rgba(255,255,255,.03)}tr:last-child td{border-bottom:0}.badge{display:inline-flex;align-items:center;height:24px;border-radius:999px;padding:0 9px;border:1px solid var(--border);font-size:.78rem}.ok{color:var(--good);background:rgba(34,197,94,.1);border-color:rgba(34,197,94,.22)}.down{color:var(--bad);background:rgba(244,63,94,.1);border-color:rgba(244,63,94,.24)}.periods{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:14px}.periods a{height:32px;padding:7px 11px;border:1px solid var(--border);border-radius:7px;text-decoration:none;color:var(--muted);font-size:.84rem}.periods a.active{background:var(--accent);border-color:var(--accent);color:#001018}.empty{color:var(--muted);margin:0;padding:16px;border:1px dashed var(--border);border-radius:8px;background:rgba(255,255,255,.025)}@media(max-width:860px){.shell{padding:18px}.topbar{display:block}.nav{justify-content:flex-start;margin-top:16px}.grid{grid-template-columns:1fr}h1{font-size:1.8rem}}
</style>
</head>
<body>
<main class="shell">
<header class="topbar">
  <div>
    <p class="eyebrow">MTProto Orchestrator</p>
    <h1>Control Dashboard</h1>
    <p class="sub">{{if .IsBridge}}Bridge routing is active. Watch each hop in the chain.{{else}}Single mode is active. Core service health is shown first.{{end}}</p>
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
    <form method="post" action="{{.PanelPath}}logout" style="display:inline"><input type="hidden" name="_csrf" class="js-csrf"><button type="submit" class="logout">Logout</button></form>
  </nav>
</header>
<section class="grid">

{{if .IsBridge}}
<article class="card span2">
<h2>Bridge Mode — Chain Health</h2>
<div class="table-wrap"><table>
<thead><tr><th>Step</th><th>Status</th><th>Latency</th><th>Message</th></tr></thead>
<tbody>
{{range .BridgeSteps}}
<tr>
  <td>{{.Name}}</td>
  <td>{{if .OK}}<span class="badge ok">ok</span>{{else}}<span class="badge down">down</span>{{end}}</td>
  <td>{{if .Latency}}{{.Latency}}{{else}}&mdash;{{end}}</td>
  <td>{{.Message}}</td>
</tr>
{{end}}
</tbody>
</table></div>
</article>
{{else}}
<article class="card span2">
<h2>Services</h2>
<div class="table-wrap"><table>
<thead><tr><th>Service</th><th>Status</th><th>Message</th></tr></thead>
<tbody>
{{range .Services}}
<tr>
  <td>{{.Name}}</td>
  <td>{{if .Active}}<span class="badge ok">running</span>{{else}}<span class="badge down">down</span>{{end}}</td>
  <td>{{.Message}}</td>
</tr>
{{end}}
</tbody>
</table></div>
</article>
{{end}}

<article class="card">
<h2>Components</h2>
<div class="table-wrap"><table>
<thead><tr><th>Component</th><th>Version</th></tr></thead>
<tbody>
{{range .Components}}
<tr><td>{{.Name}}</td><td>{{.Version}}</td></tr>
{{end}}
</tbody>
</table></div>
</article>

{{if .LiveConnections}}
<article class="card">
<h2>Active Connections</h2>
<div class="table-wrap"><table>
<thead><tr><th>User</th><th>Active Connections</th></tr></thead>
<tbody>
{{range .LiveConnections}}
<tr>
  <td>{{.UserLabel}}</td>
  <td>{{.Connections}}</td>
</tr>
{{end}}
</tbody>
</table></div>
</article>
{{end}}

<article class="card span2">
<h2>Top Users</h2>
<div class="periods">
  <a href="?period=1h"{{if eq .Period "1h"}} class="active"{{end}}>1h</a>
  <a href="?period=24h"{{if eq .Period "24h"}} class="active"{{end}}>24h</a>
  <a href="?period=7d"{{if eq .Period "7d"}} class="active"{{end}}>7d</a>
  <a href="?period=30d"{{if eq .Period "30d"}} class="active"{{end}}>30d</a>
</div>
{{if .TopUsers}}
<div class="table-wrap"><table>
<thead><tr><th>User</th><th>Bytes In</th><th>Bytes Out</th><th>Connections</th></tr></thead>
<tbody>
{{range .TopUsers}}
<tr>
  <td>{{.UserLabel}}</td>
  <td>{{.BytesIn}}</td>
  <td>{{.BytesOut}}</td>
  <td>{{.Connections}}</td>
</tr>
{{end}}
</tbody>
</table></div>
{{else}}
<p class="empty">No traffic data for this period.</p>
{{end}}
</article>
</section>
</main>
<script>(function(){var m=document.cookie.match(/(?:^|;)\s*csrf_token=([^;]+)/);if(m)document.querySelectorAll('.js-csrf').forEach(function(el){el.value=decodeURIComponent(m[1]);})})();</script>
</body>
</html>
`))

func loginPage(w io.Writer, csrfToken, errMsg string) {
	loginTmpl.Execute(w, map[string]string{ //nolint:errcheck
		"CSRFField": CSRFField(),
		"CSRFToken": csrfToken,
		"Error":     errMsg,
	})
}

func dashboardPage(w io.Writer, data DashboardData) {
	dashboardTmpl.Execute(w, data) //nolint:errcheck
}
