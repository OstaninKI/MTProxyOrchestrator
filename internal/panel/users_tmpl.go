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
	"connectionStatusLabel": func(status UserConnectionStatus) string {
		switch status {
		case UserConnectionOnline:
			return "online"
		case UserConnectionOffline:
			return "offline"
		default:
			return "not connected"
		}
	},
	"connectionStatusClass": func(status UserConnectionStatus) string {
		switch status {
		case UserConnectionOnline:
			return "status-online"
		case UserConnectionOffline:
			return "status-offline"
		default:
			return "status-never"
		}
	},
}

const userListContent = `{{define "page_title"}}Users{{end}}
{{define "content"}}
<h1>Users</h1>
<a href="{{.PanelPath}}dashboard">← Dashboard</a>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" action="{{.PanelPath}}users/create" class="user-create-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<input type="text" name="label" placeholder="label (a-z0-9_)" required>
<button type="submit">Add user</button>
</form>
<div class="table-wrap users-table-wrap">
<table class="users-table">
<thead><tr><th>Label</th><th>Status</th><th>Quota</th><th>Usage</th><th>Created</th><th>Actions</th></tr></thead>
<tbody>
{{range .Users}}
<tr>
  <td>{{.Label}}</td>
  <td>
    {{if .Enabled}}<span class="badge-on">enabled</span>{{else}}<span class="badge-off">disabled</span>{{end}}
    {{if .QuotaSuspended}} <span class="badge-off">suspended</span>{{end}}
    <div class="user-connection {{connectionStatusClass .ConnectionStatus}}">
      {{connectionStatusLabel .ConnectionStatus}}{{if gt .ActiveConnections 0}} · {{.ActiveConnections}} conn{{end}}
    </div>
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
      <div class="usage-stack">
        <strong>{{formatBytes .TrafficDownloadedBytes}}</strong>
        <span>Downloaded</span>
        <span>{{formatBytes .TrafficUploadedBytes}} Uploaded</span>
        <span>{{formatBytes .TrafficTotalBytes}} Total</span>
      </div>
    {{end}}
  </td>
  <td>{{.CreatedAt.Format "2006-01-02"}}</td>
  <td>
    <div class="user-actions">
    <form method="post" action="{{$.PanelPath}}users/{{.ID}}/toggle" class="inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">{{if .Enabled}}Disable{{else}}Enable{{end}}</button>
    </form>
    <form method="post" action="{{$.PanelPath}}users/{{.ID}}/rotate" class="inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">Rotate</button>
    </form>
    <form method="post" action="{{$.PanelPath}}users/{{.ID}}/suspend" class="inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">{{if .QuotaSuspended}}Unsuspend{{else}}Suspend{{end}}</button>
    </form>
    <form method="post" action="{{$.PanelPath}}users/{{.ID}}/quota/reset" class="inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">Reset quota</button>
    </form>
    <form method="post" action="{{$.PanelPath}}users/{{.ID}}/quota" class="quota-form">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <input type="number" step="0.1" min="0" name="gb" placeholder="GB" inputmode="decimal" class="quota-number quota-gb">
      <select name="period">
        <option value="daily">daily</option>
        <option value="weekly">weekly</option>
        <option value="monthly" selected>monthly</option>
      </select>
      <input type="number" min="0" max="100" name="warn_pct" value="80" inputmode="numeric" class="quota-number quota-warn">
      <button type="submit">Set quota</button>
    </form>
    <form method="post" action="{{$.PanelPath}}users/{{.ID}}/delete" class="inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit" onclick="return confirm('Delete {{.Label}}?')">Delete</button>
    </form>
    </div>
  </td>
</tr>
{{end}}
</tbody>
</table>
</div>
{{end}}
{{template "base" .}}`

var userListTmpl = layoutTemplate("users", userListContent, userListFuncs)

const userCreatedContent = `{{define "page_title"}}User created{{end}}
{{define "content"}}
<h1>User created</h1>
<div class="card">
<p><strong>Label:</strong> {{.Label}}</p>
<p><strong>Telegram link:</strong><br><code>{{.TelegramURL}}</code></p>
<p class="warn">⚠ Save this link — the secret will not be shown again.</p>
</div>
<p><a href="{{.PanelPath}}users">← Back to users</a></p>
{{end}}
{{template "base" .}}`

var userCreatedTmpl = layoutTemplate("user_created", userCreatedContent, nil)

type userListData struct {
	Users     []UserRow
	CSRFField string
	CSRFToken string
	Error     string
	PanelPath string
}

type userCreatedData struct {
	CSRFField   string
	CSRFToken   string
	Label       string
	TelegramURL string
	PanelPath   string
}

func userListPage(w io.Writer, users []UserRow, csrfToken, errMsg, panelPath string) {
	userListTmpl.Execute(w, userListData{
		Users:     users,
		CSRFField: CSRFField(),
		CSRFToken: csrfToken,
		Error:     errMsg,
		PanelPath: panelPath,
	})
}

func userCreatedPage(w io.Writer, label, secretHex, serverAddr string, port int, maskHost, panelPath, csrfToken string) {
	link := ProxyLink{
		Server:    serverAddr,
		Port:      port,
		SecretHex: secretHex,
		MaskHost:  maskHost,
	}
	userCreatedTmpl.Execute(w, userCreatedData{
		CSRFField:   CSRFField(),
		CSRFToken:   csrfToken,
		Label:       label,
		TelegramURL: link.TelegramURL(),
		PanelPath:   panelPath,
	})
}
