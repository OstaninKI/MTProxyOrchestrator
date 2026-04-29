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
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1{margin-bottom:1rem}a{color:#2563eb}
table{border-collapse:collapse;width:100%}th,td{text-align:left;padding:.5rem;border-bottom:1px solid #e5e7eb}
.ok{color:#16a34a}.down{color:#dc2626}
.periods{margin:.5rem 0 1rem;display:flex;gap:.5rem}.periods a{padding:.25rem .75rem;border:1px solid #ccc;border-radius:4px;text-decoration:none;color:#333}
.periods a.active{background:#2563eb;color:#fff;border-color:#2563eb}
h2{margin:1.5rem 0 .5rem}</style>
</head>
<body>
<h1>Dashboard</h1>

<h2>Services</h2>
<table>
<thead><tr><th>Service</th><th>Status</th><th>Message</th></tr></thead>
<tbody>
{{range .Services}}
<tr>
  <td>{{.Name}}</td>
  <td>{{if .Active}}<span class="ok">running</span>{{else}}<span class="down">down</span>{{end}}</td>
  <td>{{.Message}}</td>
</tr>
{{end}}
</tbody>
</table>

<h2>Top Users</h2>
<div class="periods">
  <a href="?period=1h"{{if eq .Period "1h"}} class="active"{{end}}>1h</a>
  <a href="?period=24h"{{if eq .Period "24h"}} class="active"{{end}}>24h</a>
  <a href="?period=7d"{{if eq .Period "7d"}} class="active"{{end}}>7d</a>
  <a href="?period=30d"{{if eq .Period "30d"}} class="active"{{end}}>30d</a>
</div>
{{if .TopUsers}}
<table>
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
</table>
{{else}}
<p style="color:#888">No traffic data for this period.</p>
{{end}}

<p style="margin-top:2rem"><a href="users">Users</a> &nbsp;|&nbsp; <a href="logout">Logout</a></p>
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
