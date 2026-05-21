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
	"countOnlineUsers": func(users []UserRow) int {
		count := 0
		for _, user := range users {
			if user.ConnectionStatus == UserConnectionOnline {
				count++
			}
		}
		return count
	},
	"countOfflineUsers": func(users []UserRow) int {
		count := 0
		for _, user := range users {
			if user.ConnectionStatus == UserConnectionOffline {
				count++
			}
		}
		return count
	},
	"countSuspendedUsers": func(users []UserRow) int {
		count := 0
		for _, user := range users {
			if user.QuotaSuspended {
				count++
			}
		}
		return count
	},
	"sumUserTraffic": func(users []UserRow) int64 {
		var total int64
		for _, user := range users {
			if user.TrafficTotalBytes > 0 {
				total += user.TrafficTotalBytes
			}
		}
		return total
	},
}

const userListContent = `{{define "page_title"}}Users{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Access</p>
    <h1 class="page-title">Users</h1>
    <p class="page-sub">Manage MTProto users, quota state, and active connections from one table.</p>
  </div>
  <div class="actions">
    <nav class="page-nav" aria-label="Users navigation">
      <a href="{{.PanelPath}}dashboard">Dashboard</a>
      <a href="{{.PanelPath}}settings/proxy">Proxy settings</a>
    </nav>
    <a class="page-cta" href="#create-user">Add user</a>
  </div>
</section>
<section class="page-stack">
<section class="summary-grid">
  <article class="summary-card">
    <span class="summary-label">Total users</span>
    <strong class="summary-value mono">{{len .Users}}</strong>
    <span class="summary-note">Configured in panel</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Online</span>
    <strong class="summary-value mono">{{countOnlineUsers .Users}}</strong>
    <span class="summary-note">Live Teleproxy connections</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Offline</span>
    <strong class="summary-value mono">{{countOfflineUsers .Users}}</strong>
    <span class="summary-note">Previously active but currently idle</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Suspended</span>
    <strong class="summary-value mono">{{countSuspendedUsers .Users}}</strong>
    <span class="summary-note">{{formatBytes (sumUserTraffic .Users)}} total traffic</span>
  </article>
</section>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<div id="create-user" class="card form-panel">
<form method="post" action="{{.PanelPath}}users/create" class="user-create-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<input type="text" name="label" placeholder="label (a-z0-9_)" required>
<button type="submit">Add user</button>
</form>
</div>
<div class="card users-toolbar-card" data-users-page>
<div class="card-body users-toolbar">
  <div class="users-toolbar-group">
    <label for="users-search">Search</label>
    <input id="users-search" type="text" placeholder="Find by label" data-users-role="search">
  </div>
  <div class="users-toolbar-group">
    <label for="users-status">Status</label>
    <select id="users-status" data-users-role="status">
      <option value="all">All</option>
      <option value="enabled">Enabled</option>
      <option value="disabled">Disabled</option>
      <option value="suspended">Suspended</option>
      <option value="online">Online</option>
      <option value="offline">Offline</option>
      <option value="not connected">Not connected</option>
    </select>
  </div>
  <div class="users-toolbar-group">
    <label for="users-sort">Sort</label>
    <select id="users-sort" data-users-role="sort">
      <option value="label">Label</option>
      <option value="created-desc">Newest</option>
      <option value="traffic-desc">Traffic</option>
      <option value="connections-desc">Connections</option>
    </select>
  </div>
  <span class="toolbar-spacer"></span>
  <span class="users-toolbar-count muted" data-users-role="count">{{len .Users}} users</span>
</div>
</div>
<div class="card table-card">
<div class="table-wrap users-table-wrap">
<table class="users-table">
<thead><tr><th>Label</th><th>Status</th><th>Quota</th><th>Usage</th><th>Created</th><th>Actions</th></tr></thead>
<tbody>
{{range .Users}}
<tr
  data-user-row
  data-label="{{.Label}}"
  data-enabled="{{if .Enabled}}true{{else}}false{{end}}"
  data-suspended="{{if .QuotaSuspended}}true{{else}}false{{end}}"
  data-connection="{{connectionStatusLabel .ConnectionStatus}}"
  data-created="{{.CreatedAt.Unix}}"
  data-traffic="{{if gt .TrafficTotalBytes 0}}{{.TrafficTotalBytes}}{{else}}{{.QuotaUsedBytes}}{{end}}"
  data-connections="{{.ActiveConnections}}"
>
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
    <details class="disclosure user-actions-menu">
      <summary>Actions</summary>
      <div class="user-actions-panel">
        <form method="post" action="{{$.PanelPath}}users/{{.ID}}/toggle" class="inline">
          <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
          <button type="submit">{{if .Enabled}}Disable{{else}}Enable{{end}}</button>
        </form>
        <form method="post" action="{{$.PanelPath}}users/{{.ID}}/rotate" class="inline">
          <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
          <button type="submit">Rotate secret</button>
        </form>
        <form method="post" action="{{$.PanelPath}}users/{{.ID}}/suspend" class="inline">
          <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
          <button type="submit" class="btn-warn">{{if .QuotaSuspended}}Unsuspend{{else}}Suspend{{end}}</button>
        </form>
        <form method="post" action="{{$.PanelPath}}users/{{.ID}}/quota/reset" class="inline">
          <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
          <button type="submit">Reset quota</button>
        </form>
        <details class="disclosure user-quota-menu">
          <summary>Set quota</summary>
          <form method="post" action="{{$.PanelPath}}users/{{.ID}}/quota" class="quota-form">
            <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
            <input type="number" step="0.1" min="0" name="gb" placeholder="GB" inputmode="decimal" class="quota-number quota-gb">
            <select name="period">
              <option value="daily">daily</option>
              <option value="weekly">weekly</option>
              <option value="monthly" selected>monthly</option>
            </select>
            <input type="number" min="0" max="100" name="warn_pct" value="80" inputmode="numeric" class="quota-number quota-warn">
            <button type="submit">Apply quota</button>
          </form>
        </details>
        <form method="post" action="{{$.PanelPath}}users/{{.ID}}/delete" class="inline">
          <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
          <button type="submit" class="danger" onclick="return confirm('Delete {{.Label}}?')">Delete</button>
        </form>
      </div>
    </details>
  </td>
</tr>
{{end}}
</tbody>
</table>
</div>
</div>
</section>
{{end}}
{{template "base" .}}`

var userListTmpl = layoutTemplate("users", userListContent, userListFuncs)

const userCreatedContent = `{{define "page_title"}}User created{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Access</p>
    <h1 class="page-title">User Created</h1>
    <p class="page-sub">The link below contains the generated secret and will not be shown again.</p>
  </div>
  <nav class="page-nav" aria-label="User created navigation">
    <a href="{{.PanelPath}}users">Back to users</a>
    <a href="{{.PanelPath}}dashboard">Dashboard</a>
  </nav>
</section>
<div class="card form-panel">
<p><strong>Label:</strong> {{.Label}}</p>
<p><strong>Telegram link:</strong><br><span class="mono-chip">{{.TelegramURL}}</span></p>
<p class="warn">Save this link now. The secret will not be shown again.</p>
</div>
{{end}}
{{template "base" .}}`

var userCreatedTmpl = layoutTemplate("user_created", userCreatedContent, nil)

type userListData struct {
	Users      []UserRow
	CSRFField  string
	CSRFToken  string
	Error      string
	CurrentNav string
	PanelPath  string
}

type userCreatedData struct {
	CSRFField   string
	CSRFToken   string
	Label       string
	TelegramURL string
	CurrentNav  string
	PanelPath   string
}

func userListPage(w io.Writer, users []UserRow, csrfToken, errMsg, panelPath string) {
	userListTmpl.Execute(w, userListData{
		Users:      users,
		CSRFField:  CSRFField(),
		CSRFToken:  csrfToken,
		Error:      errMsg,
		CurrentNav: "users",
		PanelPath:  panelPath,
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
		CurrentNav:  "users",
		PanelPath:   panelPath,
	})
}
