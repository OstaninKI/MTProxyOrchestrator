package panel

import (
	"html/template"
	"io"
	"time"
)

var userListFuncs = template.FuncMap{
	"formatBytes": func(n int64) string {
		if n < 0 {
			return "0 B"
		}
		return formatBytes(uint64(n))
	},
	"quotaPct": func(used, total int64) int {
		if used < 0 {
			used = 0
		}
		if total <= 0 {
			return 0
		}
		return quotaPct(uint64(used), uint64(total))
	},
	"nextResetIn": func(periodStart int64, period string) string {
		return nextResetIn(periodStart, period, time.Now())
	},
}

var userListTmpl = template.Must(template.New("users").Funcs(userListFuncs).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Users</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1{margin-bottom:1rem}
table{border-collapse:collapse;width:100%;margin-bottom:2rem}th,td{text-align:left;padding:.5rem;border-bottom:1px solid #e5e7eb}
.badge-on{color:#16a34a}.badge-off{color:#dc2626}
input[type=text]{padding:.4rem .6rem;border:1px solid #ccc;border-radius:4px;margin-right:.5rem}
button{padding:.4rem .8rem;background:#2563eb;color:#fff;border:none;border-radius:4px;cursor:pointer}
button:hover{background:#1d4ed8}.error{color:#dc2626;margin-bottom:1rem}
a{color:#2563eb}
.qbar{position:relative;width:160px;height:10px;background:#e5e7eb;border-radius:5px;overflow:hidden}
.qbar>span{display:block;height:100%;border-radius:5px;transition:width .2s}
.qbar-green>span{background:#16a34a}.qbar-amber>span{background:#d97706}.qbar-red>span{background:#dc2626}
.qmeta{font-size:.8rem;color:#555;margin-top:.15rem}.muted{color:#888}</style>
</head>
<body>
<h1>Users</h1>
<a href="../">← Dashboard</a>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" action="users/create">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<input type="text" name="label" placeholder="label (a-z0-9_)" required>
<button type="submit">Add user</button>
</form>
<table>
<thead><tr><th>Label</th><th>Status</th><th>Quota</th><th>Used</th><th>Created</th><th>Actions</th></tr></thead>
<tbody>
{{range .Users}}
<tr>
  <td>{{.Label}}</td>
  <td>
    {{if .Enabled}}<span class="badge-on">enabled</span>{{else}}<span class="badge-off">disabled</span>{{end}}
    {{if .QuotaSuspended}} <span class="badge-off">suspended</span>{{end}}
  </td>
  <td>{{if gt .QuotaBytes 0}}{{formatBytes .QuotaBytes}} / {{.QuotaPeriod}}{{else}}<span class="muted">unlimited</span>{{end}}</td>
  <td>
    {{if gt .QuotaBytes 0}}
      {{- $pct := quotaPct .QuotaUsedBytes .QuotaBytes -}}
      {{- $color := "" -}}
      {{- if or .QuotaSuspended (ge $pct 100) -}}{{- $color = "qbar-red" -}}
      {{- else if and (gt .QuotaWarnPct 0) (ge $pct .QuotaWarnPct) -}}{{- $color = "qbar-amber" -}}
      {{- else -}}{{- $color = "qbar-green" -}}{{- end -}}
      <div class="qbar {{$color}}" role="progressbar"
           aria-valuenow="{{$pct}}" aria-valuemin="0" aria-valuemax="100"
           aria-label="{{formatBytes .QuotaUsedBytes}} of {{formatBytes .QuotaBytes}} used ({{$pct}}%)">
        <span style="width:{{$pct}}%"></span>
      </div>
      <div class="qmeta">{{formatBytes .QuotaUsedBytes}} / {{formatBytes .QuotaBytes}} ({{$pct}}%)
        {{- $r := nextResetIn .QuotaPeriodStart .QuotaPeriod -}}
        {{- if $r}} · {{$r}}{{end -}}
      </div>
    {{else}}
      <span class="muted">{{formatBytes .QuotaUsedBytes}}</span>
    {{end}}
  </td>
  <td>{{.CreatedAt.Format "2006-01-02"}}</td>
  <td>
    <form method="post" action="users/{{.ID}}/toggle" style="display:inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">{{if .Enabled}}Disable{{else}}Enable{{end}}</button>
    </form>
    <form method="post" action="users/{{.ID}}/rotate" style="display:inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">Rotate</button>
    </form>
    <form method="post" action="users/{{.ID}}/suspend" style="display:inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">{{if .QuotaSuspended}}Unsuspend{{else}}Suspend{{end}}</button>
    </form>
    <form method="post" action="users/{{.ID}}/quota/reset" style="display:inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">Reset quota</button>
    </form>
    <form method="post" action="users/{{.ID}}/quota" style="display:inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <input type="number" step="0.1" min="0" name="gb" placeholder="GB" style="width:5em">
      <select name="period">
        <option value="daily">daily</option>
        <option value="weekly">weekly</option>
        <option value="monthly" selected>monthly</option>
      </select>
      <input type="number" min="0" max="100" name="warn_pct" value="80" style="width:4em">
      <button type="submit">Set quota</button>
    </form>
    <form method="post" action="users/{{.ID}}/delete" style="display:inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit" onclick="return confirm('Delete {{.Label}}?')">Delete</button>
    </form>
  </td>
</tr>
{{end}}
</tbody>
</table>
</body>
</html>
`))

var userCreatedTmpl = template.Must(template.New("user_created").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>User created</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}.card{background:#f0fdf4;border:1px solid #bbf7d0;border-radius:8px;padding:1.5rem;max-width:600px}
code{background:#e5e7eb;padding:.2rem .4rem;border-radius:3px;font-family:monospace;word-break:break-all}
a{color:#2563eb}.warn{color:#d97706;font-size:.875rem;margin-top:.5rem}</style>
</head>
<body>
<h1>User created</h1>
<div class="card">
<p><strong>Label:</strong> {{.Label}}</p>
<p><strong>Telegram link:</strong><br><code>{{.TelegramURL}}</code></p>
<p class="warn">⚠ Save this link — the secret will not be shown again.</p>
</div>
<p><a href="../users">← Back to users</a></p>
</body>
</html>
`))

type userListData struct {
	Users     []UserRow
	CSRFField string
	CSRFToken string
	Error     string
}

type userCreatedData struct {
	Label       string
	TelegramURL string
}

func userListPage(w io.Writer, users []UserRow, csrfToken, errMsg string) {
	userListTmpl.Execute(w, userListData{ //nolint:errcheck
		Users:     users,
		CSRFField: CSRFField(),
		CSRFToken: csrfToken,
		Error:     errMsg,
	})
}

func userCreatedPage(w io.Writer, label, secretHex string) {
	link := ProxyLink{
		Server:    "<your-server>",
		Port:      443,
		SecretHex: secretHex,
		MaskHost:  "www.microsoft.com",
	}
	userCreatedTmpl.Execute(w, userCreatedData{ //nolint:errcheck
		Label:       label,
		TelegramURL: link.TelegramURL(),
	})
}
