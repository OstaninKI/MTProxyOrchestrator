package panel

import (
	"io"
)

type proxySettingsData struct {
	CSRFField   string
	CSRFToken   string
	MaskHost    string
	MTProtoPort int
	ServerAddr  string
	Success     string
	Error       string
	CurrentNav  string
	PanelPath   string
}

type adminPasswordData struct {
	CSRFField  string
	CSRFToken  string
	Success    string
	Error      string
	CurrentNav string
	PanelPath  string
}

type systemSettingsData struct {
	CSRFField           string
	CSRFToken           string
	PanelPath           string
	LogLevel            string
	RetentionMinuteDays int
	RetentionHourlyDays int
	Success             string
	Error               string
	CurrentNav          string
}

const proxySettingsContent = `{{define "page_title"}}Proxy Settings{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Settings</p>
    <h1 class="page-title">Proxy Settings</h1>
    <p class="page-sub">Configure the public host, MTProto port, and the address embedded into generated user links.</p>
  </div>
</section>
<section class="page-stack">
<nav class="settings-tabs" aria-label="Settings tabs">
  <a class="active" href="{{.PanelPath}}settings/proxy">Endpoint &amp; Proxy</a>
  <a href="{{.PanelPath}}settings/admin-password">Admin password</a>
  <a href="{{.PanelPath}}settings/system">System</a>
  <a href="{{.PanelPath}}settings/totp">Two-factor</a>
</nav>

{{if .Success}}<p class="success">{{.Success}}</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<section class="summary-grid">
  <article class="summary-card">
    <span class="summary-label">Public endpoint</span>
    <strong class="summary-value mono">{{.ServerAddr}}:{{.MTProtoPort}}</strong>
    <span class="summary-note">Embedded into generated MTProto links</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Mask host</span>
    <strong class="summary-value mono">{{.MaskHost}}</strong>
    <span class="summary-note">Camouflage host used by Teleproxy</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Link format</span>
    <strong class="summary-value">tg://proxy</strong>
    <span class="summary-note">User secrets remain write-only in panel storage</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Edge port</span>
    <strong class="summary-value mono">{{.MTProtoPort}}</strong>
    <span class="summary-note">Public MTProto listener exposed by the node</span>
  </article>
</section>

<div class="stack-split">
<div class="card form-panel">
<div class="card-body">
<h2>Public Endpoint</h2>
<p class="panel-note">These values define what address is advertised to Telegram clients and what camouflage host Teleproxy presents on the edge.</p>
</div>
<form method="post" class="stack-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">

<label for="mask_host">Mask host</label>
<input type="text" id="mask_host" name="mask_host" value="{{.MaskHost}}" required>
<p class="field-hint">Use a stable HTTPS host that resolves publicly and tolerates front-facing traffic patterns.</p>

<label for="mtproto_port">MTProto port</label>
<input type="number" id="mtproto_port" name="mtproto_port" value="{{.MTProtoPort}}" min="1" max="65535" required>
<p class="field-hint">Keep this aligned with firewall exposure and installation-time ingress configuration.</p>

<label for="server_addr">Server IP or domain (used in proxy links)</label>
<input type="text" id="server_addr" name="server_addr" value="{{.ServerAddr}}" placeholder="e.g. 1.2.3.4 or proxy.example.com">
<p class="field-hint">This value is what clients import. Prefer the exact public address reachable by Telegram users.</p>

<button type="submit">Save</button>
</form>
</div>
<aside class="card side-panel">
<h2>Endpoint Preview</h2>
<div class="summary-list">
  <div class="summary-row">
    <span class="badge ok">MTProto</span>
    <span class="summary-copy"><strong class="mono">{{.ServerAddr}}:{{.MTProtoPort}}</strong><span>Address embedded into generated user links</span></span>
  </div>
  <div class="summary-row">
    <span class="badge">Mask host</span>
    <span class="summary-copy"><strong class="mono">{{.MaskHost}}</strong><span>Camouflage host used by Teleproxy</span></span>
  </div>
  <div class="summary-row">
    <span class="badge warn">Share link</span>
    <span class="summary-copy"><strong class="mono">tg://proxy?server={{.ServerAddr}}&amp;port={{.MTProtoPort}}&amp;secret=ee...</strong><span>Preview only; secret remains write-only</span></span>
  </div>
  <div class="summary-row">
    <span class="badge">Scope</span>
    <span class="summary-copy"><strong>Panel-managed</strong><span>Saving here updates panel-visible defaults without exposing per-user secrets.</span></span>
  </div>
</div>
</aside>
</div>
</section>
{{end}}
{{template "base" .}}`

var proxySettingsTmpl = layoutTemplate("proxy_settings", proxySettingsContent, nil)

const adminPasswordContent = `{{define "page_title"}}Change Admin Password{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Settings</p>
    <h1 class="page-title">Change Admin Password</h1>
    <p class="page-sub">Rotate the panel administrator password. Secrets remain write-only and are never echoed back.</p>
  </div>
</section>
<section class="page-stack">
<nav class="settings-tabs" aria-label="Settings tabs">
  <a href="{{.PanelPath}}settings/proxy">Endpoint &amp; Proxy</a>
  <a class="active" href="{{.PanelPath}}settings/admin-password">Admin password</a>
  <a href="{{.PanelPath}}settings/system">System</a>
  <a href="{{.PanelPath}}settings/totp">Two-factor</a>
</nav>

{{if .Success}}<p class="success">{{.Success}}</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<section class="summary-grid">
  <article class="summary-card">
    <span class="summary-label">Minimum length</span>
    <strong class="summary-value mono">16+</strong>
    <span class="summary-note">Backend validation rejects shorter passwords</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Composition</span>
    <strong class="summary-value">Letters + digits</strong>
    <span class="summary-note">Simple weak phrases are not accepted</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Storage</span>
    <strong class="summary-value">bcrypt</strong>
    <span class="summary-note">Secrets are never echoed back by the panel</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Session impact</span>
    <strong class="summary-value">Rotate</strong>
    <span class="summary-note">Active session is invalidated after password change</span>
  </article>
</section>

<div class="stack-split" data-password-page>
<div class="card form-panel">
<div class="card-body">
<h2>Rotate Credentials</h2>
<p class="panel-note">Use a long operator-only password. The panel validates length and composition before hashing and replacing the stored credential.</p>
</div>
<form method="post" class="stack-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">

<label for="current_password">Current password</label>
<div class="password-field">
  <input type="password" id="current_password" name="current_password" required data-password-role="field">
  <button type="button" class="password-toggle" data-password-role="toggle" data-password-target="current_password">Show</button>
</div>

<label for="new_password">New password</label>
<div class="password-field">
  <input type="password" id="new_password" name="new_password" required data-password-role="field" data-password-strength-source>
  <button type="button" class="password-toggle" data-password-role="toggle" data-password-target="new_password">Show</button>
</div>
<div class="password-meter">
  <div class="ops-meter" data-password-role="strength-meter" data-tone="danger"><span style="width: 0%"></span></div>
  <div class="field-hint" data-password-role="strength-note">Minimum 16 characters, must contain letters and digits.</div>
</div>

<label for="confirm_password">Confirm password</label>
<div class="password-field">
  <input type="password" id="confirm_password" name="confirm_password" required data-password-role="field" data-password-confirm>
  <button type="button" class="password-toggle" data-password-role="toggle" data-password-target="confirm_password">Show</button>
</div>
<div class="field-hint" data-password-role="match-note">Repeat the new password exactly.</div>

<button type="submit">Change Password</button>
</form>
</div>
<aside class="card side-panel">
<h2>Rotation Notes</h2>
<div class="summary-list">
  <div class="summary-row">
    <span class="badge ok">Length</span>
    <span class="summary-copy"><strong>16+ characters</strong><span>Required by backend validation.</span></span>
  </div>
  <div class="summary-row">
    <span class="badge warn">Composition</span>
    <span class="summary-copy"><strong>Letters and digits</strong><span>Weak phrases are rejected.</span></span>
  </div>
  <div class="summary-row">
    <span class="badge">Session</span>
    <span class="summary-copy"><strong>Current session rotates</strong><span>Existing session cookie is invalidated after change.</span></span>
  </div>
  <div class="summary-row">
    <span class="badge ok">Storage</span>
    <span class="summary-copy"><strong>Hashed only</strong><span>The panel persists a password hash and never stores the raw secret.</span></span>
  </div>
</div>
</aside>
</div>
</section>
{{end}}
{{template "base" .}}`

var adminPasswordTmpl = layoutTemplate("admin_password", adminPasswordContent, nil)

const systemSettingsContent = `{{define "page_title"}}System Settings{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Settings</p>
    <h1 class="page-title">System Settings</h1>
    <p class="page-sub">Adjust panel path, log level, and retention windows for panel-managed logs.</p>
  </div>
</section>
<section class="page-stack">
<nav class="settings-tabs" aria-label="Settings tabs">
  <a href="{{.PanelPath}}settings/proxy">Endpoint &amp; Proxy</a>
  <a href="{{.PanelPath}}settings/admin-password">Admin password</a>
  <a class="active" href="{{.PanelPath}}settings/system">System</a>
  <a href="{{.PanelPath}}settings/totp">Two-factor</a>
</nav>

{{if .Success}}<p class="success">{{.Success}}</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<div class="warn-box">
<strong>Note:</strong> Panel path and log level changes require restarting tgproxy-panel to take effect.
</div>

<section class="summary-grid">
  <article class="summary-card">
    <span class="summary-label">Panel path</span>
    <strong class="summary-value mono">{{.PanelPath}}</strong>
    <span class="summary-note">Base path expected by cookies, routes, and operator bookmarks</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Log level</span>
    <strong class="summary-value">{{.LogLevel}}</strong>
    <span class="summary-note">Controls panel verbosity for operator-facing diagnostics</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Minute retention</span>
    <strong class="summary-value mono">{{.RetentionMinuteDays}}d</strong>
    <span class="summary-note">Short-window derived summaries kept in the DB</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Hourly retention</span>
    <strong class="summary-value mono">{{.RetentionHourlyDays}}d</strong>
    <span class="summary-note">Long-window rollups kept for dashboard history</span>
  </article>
</section>

<form method="post" class="page-stack settings-form-grid">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">

<div class="stack-split">
<div class="page-stack">
<div class="card">
<div class="card-body">
<h2>Panel Access</h2>
<p class="panel-note">These values control where the panel is mounted and what base path is expected by cookies and routes.</p>
</div>
<label for="panel_path">Panel path</label>
<input type="text" id="panel_path" name="panel_path" value="{{.PanelPath}}" required>
<p class="field-hint">Changing this path affects login, cookies, links, and every bookmarked admin route.</p>
</div>

<div class="card">
<div class="card-body">
<h2>Logging</h2>
<p class="panel-note">These values control panel log verbosity and how long derived minute and hourly summaries are retained.</p>
</div>
<label for="log_level">Log level</label>
<select id="log_level" name="log_level" required>
<option value="debug" {{if eq .LogLevel "debug"}}selected{{end}}>debug</option>
<option value="info" {{if eq .LogLevel "info"}}selected{{end}}>info</option>
<option value="warn" {{if eq .LogLevel "warn"}}selected{{end}}>warn</option>
<option value="error" {{if eq .LogLevel "error"}}selected{{end}}>error</option>
</select>
<p class="field-hint">Use debug only for short troubleshooting windows; keep info or higher during normal operation.</p>

<label for="retention_minutes">Log retention (minutes): days</label>
<input type="number" id="retention_minutes" name="retention_minutes" value="{{.RetentionMinuteDays}}" min="1" max="30" required>
<p class="field-hint">Short retention keeps minute-level buckets compact and improves steady-state storage behavior.</p>

<label for="retention_hourly">Log retention (hourly): days</label>
<input type="number" id="retention_hourly" name="retention_hourly" value="{{.RetentionHourlyDays}}" min="7" max="365" required>
<p class="field-hint">Hourly summaries support longer dashboard windows without preserving every fine-grained sample.</p>
</div>

<div class="settings-submit-row">
<button type="submit">Save</button>
</div>
</div>
<aside class="card side-panel">
<h2>Operational Notes</h2>
<div class="summary-list">
  <div class="summary-row">
    <span class="badge warn">Restart</span>
    <span class="summary-copy"><strong>Path and level changes</strong><span>tgproxy-panel restart is required before these settings become effective.</span></span>
  </div>
  <div class="summary-row">
    <span class="badge">Retention</span>
    <span class="summary-copy"><strong>Dashboard history</strong><span>Minute and hourly windows shape how much history the dashboard can summarize efficiently.</span></span>
  </div>
  <div class="summary-row">
    <span class="badge ok">Scope</span>
    <span class="summary-copy"><strong>Panel only</strong><span>These settings do not reconfigure Teleproxy user secrets or Bridge node definitions.</span></span>
  </div>
</div>
</aside>
</div>
</form>
</section>
{{end}}
{{template "base" .}}`

var systemSettingsTmpl = layoutTemplate("system_settings", systemSettingsContent, nil)

func proxySettingsPage(w io.Writer, data proxySettingsData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "settings"
	}
	proxySettingsTmpl.Execute(w, data)
}

func adminPasswordPage(w io.Writer, data adminPasswordData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "settings"
	}
	adminPasswordTmpl.Execute(w, data)
}

func systemSettingsPage(w io.Writer, data systemSettingsData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "settings"
	}
	systemSettingsTmpl.Execute(w, data)
}
