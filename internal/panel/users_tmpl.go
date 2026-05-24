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
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Users</h1>
    <p class="page-sub">Manage MTProto users, quotas and access.</p>
  </div>
  <div class="actions">
    <button class="btn" data-variant="primary" type="button" data-users-open-create>{{icon "Plus" 13}} Add user</button>
  </div>
</section>
<section class="page-stack">
<section class="grid-12">
  <article class="col-3 card stat-card">
    <div class="card-body">
      <div class="stat-head"><span class="stat-icon">{{icon "Users" 15}}</span><span class="stat-label">Total</span></div>
      <strong class="stat-value mono">{{len .Users}}</strong>
      <span class="stat-hint">All users</span>
    </div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body">
      <div class="stat-head"><span class="stat-icon" data-tone="success">{{icon "Activity" 15}}</span><span class="stat-label">Online</span></div>
      <strong class="stat-value mono">{{countOnlineUsers .Users}}</strong>
      <span class="stat-hint">Live Teleproxy connections</span>
    </div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body">
      <div class="stat-head"><span class="stat-icon">{{icon "Power" 15}}</span><span class="stat-label">Offline</span></div>
      <strong class="stat-value mono">{{countOfflineUsers .Users}}</strong>
      <span class="stat-hint">Last seen or idle</span>
    </div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body">
      <div class="stat-head"><span class="stat-icon" data-tone="warn">{{icon "Pause" 15}}</span><span class="stat-label">Suspended</span></div>
      <strong class="stat-value mono">{{countSuspendedUsers .Users}}</strong>
      <span class="stat-hint">{{formatBytes (sumUserTraffic .Users)}} total traffic</span>
    </div>
  </article>
</section>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<div data-users-page>
<div class="card users-toolbar-card">
<div class="card-body users-toolbar">
  <div class="users-toolbar-group users-toolbar-search">
    <div class="input-group">{{icon "Search" 13}}<input class="input" id="users-search" type="text" placeholder="Search users by label..." data-users-role="search"></div>
  </div>
  <div class="users-filter-seg" data-users-role="status-buttons">
    <button class="seg-item active" type="button" data-users-status-value="all">All ({{len .Users}})</button>
    <button class="seg-item" type="button" data-users-status-value="online">Online ({{countOnlineUsers .Users}})</button>
    <button class="seg-item" type="button" data-users-status-value="offline">Offline ({{countOfflineUsers .Users}})</button>
    <button class="seg-item" type="button" data-users-status-value="suspended">Suspended ({{countSuspendedUsers .Users}})</button>
  </div>
  <select class="select sr-only" id="users-status" data-users-role="status">
      <option value="all">All</option>
      <option value="enabled">Enabled</option>
      <option value="disabled">Disabled</option>
      <option value="suspended">Suspended</option>
      <option value="online">Online</option>
      <option value="offline">Offline</option>
      <option value="not connected">Not connected</option>
  </select>
  <div class="users-toolbar-group users-toolbar-sort">
    <select class="select" id="users-sort" data-users-role="sort">
      <option value="label">Label</option>
      <option value="created-desc">Newest</option>
      <option value="traffic-desc">Traffic</option>
      <option value="connections-desc">Connections</option>
    </select>
  </div>
  <span class="toolbar-spacer"></span>
  <button class="btn" data-size="sm" data-variant="ghost" type="button" disabled>{{icon "Download" 13}} Export</button>
  <span class="users-toolbar-count muted" data-users-role="count">{{len .Users}} users</span>
</div>
</div>
<div class="card table-card">
<div class="table-wrap users-table-wrap">
<table class="tbl users-table">
<thead><tr><th>Label</th><th>Status</th><th>Quota</th><th>Usage</th><th>Created</th><th>Actions</th></tr></thead>
<tbody>
{{range .Users}}
<tr
  data-user-row
  data-id="{{.ID}}"
  data-label="{{.Label}}"
  data-enabled="{{if .Enabled}}true{{else}}false{{end}}"
  data-suspended="{{if .QuotaSuspended}}true{{else}}false{{end}}"
  data-connection="{{connectionStatusLabel .ConnectionStatus}}"
  data-created-label="{{.CreatedAt.Format "2006-01-02"}}"
  data-created="{{.CreatedAt.Unix}}"
  data-traffic="{{if gt .TrafficTotalBytes 0}}{{.TrafficTotalBytes}}{{else}}{{.QuotaUsedBytes}}{{end}}"
  data-connections="{{.ActiveConnections}}"
  data-status-label="{{if .Enabled}}enabled{{else}}disabled{{end}}{{if .QuotaSuspended}}, suspended{{end}}"
  data-quota-limit="{{if gt .QuotaBytes 0}}{{formatBytes .QuotaBytes}}{{else}}Unlimited{{end}}"
  data-quota-period="{{if .QuotaPeriod}}{{.QuotaPeriod}}{{else}}—{{end}}"
  data-quota-used="{{if gt .QuotaBytes 0}}{{formatBytes .QuotaUsedBytes}}{{else}}{{formatBytes .TrafficTotalBytes}}{{end}}"
  data-quota-percent="{{if gt .QuotaBytes 0}}{{quotaPct .QuotaUsedBytes .QuotaBytes}}{{else}}0{{end}}"
  data-quota-reset="{{if gt .QuotaBytes 0}}{{nextResetIn .QuotaPeriodStart .QuotaPeriod}}{{else}}—{{end}}"
  data-download="{{formatBytes .TrafficDownloadedBytes}}"
  data-upload="{{formatBytes .TrafficUploadedBytes}}"
  data-total="{{formatBytes .TrafficTotalBytes}}"
  data-toggle-url="{{$.PanelPath}}users/{{.ID}}/toggle"
  data-toggle-label="{{if .Enabled}}Disable{{else}}Enable{{end}}"
  data-rotate-url="{{$.PanelPath}}users/{{.ID}}/rotate"
  data-suspend-url="{{$.PanelPath}}users/{{.ID}}/suspend"
  data-suspend-label="{{if .QuotaSuspended}}Unsuspend{{else}}Suspend{{end}}"
  data-reset-url="{{$.PanelPath}}users/{{.ID}}/quota/reset"
  data-quota-url="{{$.PanelPath}}users/{{.ID}}/quota"
  data-delete-url="{{$.PanelPath}}users/{{.ID}}/delete"
>
  <td>
    <button class="user-link-btn" type="button" data-user-open>
      <span class="user-link-copy">
        <strong>{{.Label}}</strong>
        <span class="user-link-meta mono">{{connectionStatusLabel .ConnectionStatus}}{{if gt .ActiveConnections 0}} · {{.ActiveConnections}} conn{{end}}</span>
      </span>
    </button>
  </td>
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
      <progress class="qbar {{$color}}"
           aria-valuenow="{{$pct}}" aria-valuemin="0" aria-valuemax="100"
           aria-label="{{formatBytes .QuotaUsedBytes}} of {{formatBytes .QuotaBytes}} used ({{$pct}}%)"
           value="{{$pct}}" max="100"></progress>
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
</div>

<div class="modal-scrim" data-users-modal hidden>
  <div class="modal" role="dialog" aria-modal="true" aria-labelledby="users-create-title">
    <form method="post" action="{{.PanelPath}}users/create">
      <div class="modal-head">
        <h2 id="users-create-title">Add user</h2>
        <p>Create a new MTProto user and issue the first access link.</p>
      </div>
      <div class="modal-body">
        <div class="field">
          <label class="label" for="users-create-label">Label</label>
          <input class="input input--mono" id="users-create-label" type="text" name="label" placeholder="e.g. alice_laptop" required>
          <p class="field-hint">Use a stable label for audit logs, quota tracking, and Bridge sync.</p>
        </div>
        <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}" class="js-csrf">
      </div>
      <div class="modal-foot">
        <button class="btn" data-variant="ghost" type="button" data-users-close-create>Cancel</button>
        <button class="btn" data-variant="primary" type="submit">{{icon "Plus" 13}} Add user</button>
      </div>
    </form>
  </div>
</div>

<div class="drawer-scrim" data-users-drawer-scrim hidden></div>
<aside class="drawer" data-users-drawer hidden aria-labelledby="users-detail-title">
  <div class="drawer-head">
    <div class="col col-zero col-fill">
      <h2 id="users-detail-title" data-users-detail="label">User</h2>
      <span class="muted mono muted-sm" data-users-detail="meta">created —</span>
    </div>
    <button class="btn" data-size="sm" data-variant="ghost" type="button" data-users-close-drawer>{{icon "X" 14}}</button>
  </div>
  <div class="drawer-body">
    <section class="detail-summary">
      <div class="detail-summary-badges">
        <span class="badge" data-tone="accent" data-users-detail="status">enabled</span>
        <span class="badge" data-tone="accent" data-users-detail="connection">not connected</span>
      </div>
      <div class="detail-quota">
        <div class="detail-quota-head">
          <span data-users-detail="quota-used">0 B</span>
          <span data-users-detail="quota-limit">Unlimited</span>
        </div>
        <progress class="bar" data-tone="success" max="100" value="0" data-users-detail="quota-bar"></progress>
        <span class="muted-sm" data-users-detail="quota-reset">—</span>
      </div>
      <div class="detail-metrics">
        <div class="detail-metric">
          <div class="detail-metric-label">Download</div>
          <div class="detail-metric-value mono" data-users-detail="download">0 B</div>
        </div>
        <div class="detail-metric">
          <div class="detail-metric-label">Upload</div>
          <div class="detail-metric-value mono" data-users-detail="upload">0 B</div>
        </div>
        <div class="detail-metric">
          <div class="detail-metric-label">Connections</div>
          <div class="detail-metric-value mono" data-users-detail="connections">0</div>
        </div>
      </div>
    </section>

    <section class="detail-section">
      <h3 class="detail-section-title">Actions</h3>
      <div class="detail-actions">
        <form method="post" action="" data-users-form="toggle">
          <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}" class="js-csrf">
          <button class="btn" data-variant="ghost" type="submit" data-users-action-label="toggle">Disable</button>
        </form>
        <form method="post" action="" data-users-form="rotate">
          <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}" class="js-csrf">
          <button class="btn" data-variant="ghost" type="submit">Rotate secret</button>
        </form>
        <form method="post" action="" data-users-form="suspend">
          <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}" class="js-csrf">
          <button class="btn btn-warn" type="submit" data-users-action-label="suspend">Suspend</button>
        </form>
        <form method="post" action="" data-users-form="reset">
          <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}" class="js-csrf">
          <button class="btn" data-variant="ghost" type="submit">Reset quota</button>
        </form>
      </div>
    </section>

    <section class="detail-section">
      <h3 class="detail-section-title">Details</h3>
      <div class="detail-list">
        <div class="detail-row"><span class="detail-row-label">Created</span><span class="detail-row-value mono" data-users-detail="created">—</span></div>
        <div class="detail-row"><span class="detail-row-label">Quota period</span><span class="detail-row-value" data-users-detail="quota-period">—</span></div>
        <div class="detail-row"><span class="detail-row-label">Total traffic</span><span class="detail-row-value mono" data-users-detail="total">0 B</span></div>
      </div>
    </section>

    <section class="detail-section">
      <h3 class="detail-section-title">Access Material</h3>
      <div class="detail-list">
        <div class="copy-row detail-placeholder">
          <span class="val mono">tg://proxy?server=...&amp;secret=ee...</span>
          <button class="btn" data-size="xs" data-variant="ghost" type="button" disabled>{{icon "Copy" 12}}</button>
        </div>
        <div class="copy-row detail-placeholder">
          <span class="val mono">Secret is intentionally not shown again</span>
          <button class="btn" data-size="xs" data-variant="ghost" type="button" disabled>{{icon "Copy" 12}}</button>
        </div>
      </div>
    </section>

    <section class="detail-section">
      <h3 class="detail-section-title">Set quota</h3>
      <form method="post" action="" class="quota-form" data-users-form="quota">
        <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}" class="js-csrf">
        <input type="number" step="0.1" min="0" name="gb" placeholder="GB" inputmode="decimal" class="quota-number quota-gb">
        <select name="period">
          <option value="daily">daily</option>
          <option value="weekly">weekly</option>
          <option value="monthly" selected>monthly</option>
        </select>
        <input type="number" min="0" max="100" name="warn_pct" value="80" inputmode="numeric" class="quota-number quota-warn">
        <button class="btn" data-variant="primary" type="submit">Apply quota</button>
      </form>
    </section>

    <section class="detail-section">
      <h3 class="detail-section-title">Danger Zone</h3>
      <form method="post" action="" data-users-form="delete">
        <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}" class="js-csrf">
        <button class="btn danger" type="submit" data-users-delete>Delete user</button>
      </form>
    </section>
  </div>
</aside>
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
  <div class="actions"><a class="btn" data-variant="ghost" href="{{.PanelPath}}users">Back to users</a></div>
</section>
<section class="grid-12">
  <div class="col-7">
    <div class="card">
      <div class="card-head"><div class="col card-title-stack"><h3>Access material</h3><span class="sub">Shown once immediately after creation.</span></div></div>
      <div class="card-body col col-panel">
        <div class="detail-row"><span class="detail-row-label">Label</span><span class="detail-row-value mono">{{.Label}}</span></div>
        <div class="detail-section">
          <h3 class="detail-section-title">Telegram link</h3>
          <div class="copy-row"><span class="val mono">{{.TelegramURL}}</span><button class="btn" data-size="xs" data-variant="ghost" data-copy type="button" disabled>{{icon "Copy" 12}}</button></div>
        </div>
        <div class="warn-box"><strong>Save this link now.</strong> The secret will not be shown again after you leave this page.</div>
      </div>
    </div>
  </div>
  <div class="col-5">
    <div class="card">
      <div class="card-head"><h3>Next steps</h3></div>
      <div class="card-body col col-panel">
        <div class="totp-note-row"><span class="badge ok">Deliver</span><span class="col totp-note-copy"><strong class="totp-note-title">Send securely</strong><span class="help">Use a trusted channel to deliver the link to the user.</span></span></div>
        <div class="totp-note-row"><span class="badge warn">Rotate</span><span class="col totp-note-copy"><strong class="totp-note-title">Compromise response</strong><span class="help">Rotate the secret from the Users page if you suspect disclosure.</span></span></div>
      </div>
    </div>
  </div>
</section>
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
