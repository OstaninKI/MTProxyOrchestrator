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
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Settings</h1>
    <p class="page-sub">Endpoint, password and system configuration.</p>
  </div>
</section>
<section class="page-stack">
<nav class="seg" aria-label="Settings tabs">
  <a class="seg-item active" href="{{.PanelPath}}settings/proxy">Endpoint &amp; Proxy</a>
  <a class="seg-item" href="{{.PanelPath}}settings/admin-password">Admin password</a>
  <a class="seg-item" href="{{.PanelPath}}settings/system">System</a>
  <a class="seg-item" href="{{.PanelPath}}settings/totp">Two-factor</a>
</nav>

{{if .Success}}<p class="success">{{.Success}}</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<div class="grid-12">
  <div class="col-7">
    <section class="card">
      <div class="card-head"><div class="col card-title-stack"><h3>MTProto endpoint</h3><span class="sub">What Telegram clients connect to</span></div></div>
      <div class="card-body">
        <form method="post" class="stack-form">
        <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
        <div class="field"><label class="label" for="mask_host">Mask host</label><input class="input input--mono" type="text" id="mask_host" name="mask_host" value="{{.MaskHost}}" required><span class="help">Used as fake SNI for camouflage.</span></div>
        <div class="grid-3">
          <div class="field"><label class="label" for="mtproto_port">MTProto port</label><input class="input input--mono" type="number" id="mtproto_port" name="mtproto_port" value="{{.MTProtoPort}}" min="1" max="65535" required></div>
          <div class="field span2"><label class="label" for="server_addr">Server IP / domain</label><input class="input input--mono" type="text" id="server_addr" name="server_addr" value="{{.ServerAddr}}" placeholder="e.g. 1.2.3.4 or proxy.example.com"><span class="help">Used in generated share links.</span></div>
        </div>
        <div class="field"><span class="label">Preview share link</span><div class="copy-row"><span class="val mono">tg://proxy?server={{.ServerAddr}}&amp;port={{.MTProtoPort}}&amp;secret=ee...</span><button class="btn" data-size="xs" data-variant="ghost" type="button" disabled>{{icon "Copy" 12}}</button></div></div>
        <div class="row row-end row-tight"><button class="btn" data-variant="ghost" type="reset">Reset</button><button class="btn" data-variant="primary" type="submit">{{icon "Check" 12}} Save changes</button></div>
        </form>
      </div>
    </section>
    <section class="card">
      <div class="card-head"><h3>Connection limits</h3></div>
      <div class="card-body grid-3">
        <div class="field"><span class="label">Max conn / user</span><input class="input input--mono" value="64" disabled></div>
        <div class="field"><span class="label">Max conn / IP</span><input class="input input--mono" value="32" disabled></div>
        <div class="field"><span class="label">Idle timeout (s)</span><input class="input input--mono" value="300" disabled></div>
      </div>
    </section>
  </div>
  <div class="col-5">
    <section class="card">
      <div class="card-head"><div class="col card-title-stack"><h3>Endpoint preview</h3><span class="sub">What's exposed to the internet</span></div></div>
      <div class="card-body col col-panel">
        <div class="endpoint-row">{{icon "Lock" 14}}<div class="col endpoint-copy"><span class="endpoint-label">MTProto + TLS</span><span class="endpoint-value mono">{{.ServerAddr}}:{{.MTProtoPort}}</span></div></div>
        <div class="endpoint-row">{{icon "Globe" 14}}<div class="col endpoint-copy"><span class="endpoint-label">Stub HTTPS</span><span class="endpoint-value mono">https://{{.MaskHost}}</span></div></div>
        <div class="endpoint-row">{{icon "Globe" 14}}<div class="col endpoint-copy"><span class="endpoint-label">Stub HTTP</span><span class="endpoint-value mono">http://{{.MaskHost}}</span></div></div>
      </div>
    </section>
    <section class="card">
      <div class="card-head"><h3>Tips</h3></div>
      <div class="card-body">
        <ul class="help-list">
          <li>Use a port that is plausible for the mask host.</li>
          <li>The mask host should resolve to a real, popular service.</li>
          <li>Server IP/domain is what ends up in share links.</li>
        </ul>
      </div>
    </section>
  </div>
</div>
</section>
{{end}}
{{template "base" .}}`

var proxySettingsTmpl = layoutTemplate("proxy_settings", proxySettingsContent, nil)

const adminPasswordContent = `{{define "page_title"}}Change Admin Password{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Settings</h1>
    <p class="page-sub">Endpoint, password and system configuration.</p>
  </div>
</section>
<section class="page-stack">
<nav class="seg" aria-label="Settings tabs">
  <a class="seg-item" href="{{.PanelPath}}settings/proxy">Endpoint &amp; Proxy</a>
  <a class="seg-item active" href="{{.PanelPath}}settings/admin-password">Admin password</a>
  <a class="seg-item" href="{{.PanelPath}}settings/system">System</a>
  <a class="seg-item" href="{{.PanelPath}}settings/totp">Two-factor</a>
</nav>

{{if .Success}}<p class="success">{{.Success}}</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<div class="grid-12" data-password-page>
  <div class="col-7">
    <section class="card">
      <div class="card-head"><div class="col card-title-stack"><h3>Change admin password</h3><span class="sub">At least 16 characters, with letters and digits.</span></div></div>
      <div class="card-body">
        <form method="post" class="stack-form">
        <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
        <div class="field"><label class="label" for="current_password">Current password</label><div class="password-field"><input class="input input--mono" type="password" id="current_password" name="current_password" required data-password-role="field"><button type="button" class="password-toggle" data-password-role="toggle" data-password-target="current_password">Show</button></div></div>
        <div class="field"><label class="label" for="new_password">New password</label><div class="password-field"><input class="input input--mono" type="password" id="new_password" name="new_password" required data-password-role="field" data-password-strength-source><button type="button" class="password-toggle" data-password-role="toggle" data-password-target="new_password">Show</button></div><div class="password-meter"><div class="ops-meter" data-password-role="strength-meter" data-tone="danger"><span class="meter-fill pct-0"></span></div><div class="help" data-password-role="strength-note">Minimum 16 characters, must contain letters and digits.</div></div></div>
        <div class="field"><label class="label" for="confirm_password">Confirm new password</label><div class="password-field"><input class="input input--mono" type="password" id="confirm_password" name="confirm_password" required data-password-role="field" data-password-confirm><button type="button" class="password-toggle" data-password-role="toggle" data-password-target="confirm_password">Show</button></div><span class="help" data-password-role="match-note">Repeat the new password exactly.</span></div>
        <div class="row row-end row-tight"><button class="btn" data-variant="ghost" type="reset">Cancel</button><button class="btn" data-variant="primary" type="submit">{{icon "Lock" 12}} Change password</button></div>
        </form>
      </div>
    </section>
  </div>
  <div class="col-5">
    <section class="card">
      <div class="card-head"><h3>Sessions</h3></div>
      <div class="card-body col col-panel">
        <div class="session-row">{{icon "Globe" 14}}<div class="col session-copy"><span class="session-title">Current browser</span><span class="help mono">active session · now</span></div><span class="badge" data-tone="success">this device</span></div>
        <div class="session-row">{{icon "Globe" 14}}<div class="col session-copy"><span class="session-title">Other sessions</span><span class="help mono">session inventory placeholder</span></div><button class="btn" data-size="xs" data-variant="ghost" disabled>Revoke</button></div>
      </div>
    </section>
    <section class="card">
      <div class="card-body">
        <div class="row">
          {{icon "Shield" 20}}
          <div class="col col-tight col-fill"><span class="setting-title">Two-factor authentication</span><span class="help">Add TOTP-based 2FA for the admin account.</span></div>
          <a class="btn" data-size="sm" href="{{.PanelPath}}settings/totp">Open</a>
        </div>
      </div>
    </section>
  </div>
</div>
</section>
{{end}}
{{template "base" .}}`

var adminPasswordTmpl = layoutTemplate("admin_password", adminPasswordContent, nil)

const systemSettingsContent = `{{define "page_title"}}System Settings{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Settings</h1>
    <p class="page-sub">Endpoint, password and system configuration.</p>
  </div>
</section>
<section class="page-stack">
<nav class="seg" aria-label="Settings tabs">
  <a class="seg-item" href="{{.PanelPath}}settings/proxy">Endpoint &amp; Proxy</a>
  <a class="seg-item" href="{{.PanelPath}}settings/admin-password">Admin password</a>
  <a class="seg-item active" href="{{.PanelPath}}settings/system">System</a>
  <a class="seg-item" href="{{.PanelPath}}settings/totp">Two-factor</a>
</nav>

{{if .Success}}<p class="success">{{.Success}}</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<div class="warn-box">
<strong>Note:</strong> Panel path and log level changes require restarting tgproxy-panel to take effect.
</div>

<form method="post" class="grid-12">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
  <div class="col-7">
    <section class="card">
      <div class="card-head"><div class="col card-title-stack"><h3>Panel access</h3><span class="sub">Hide the admin panel behind a hard-to-guess prefix.</span></div></div>
      <div class="card-body">
        <div class="field"><label class="label" for="panel_path">Panel path</label><input class="input input--mono" type="text" id="panel_path" name="panel_path" value="{{.PanelPath}}" required><span class="help">Changing this path affects login, cookies, links, and bookmarked admin routes.</span></div>
      </div>
    </section>
    <section class="card">
      <div class="card-head"><div class="col card-title-stack"><h3>Logging</h3><span class="sub">Changes require restarting tgproxy-panel.</span></div></div>
      <div class="card-body col col-panel">
        <div class="field"><label class="label" for="log_level">Log level</label><select class="select" id="log_level" name="log_level" required><option value="debug" {{if eq .LogLevel "debug"}}selected{{end}}>debug</option><option value="info" {{if eq .LogLevel "info"}}selected{{end}}>info</option><option value="warn" {{if eq .LogLevel "warn"}}selected{{end}}>warn</option><option value="error" {{if eq .LogLevel "error"}}selected{{end}}>error</option></select></div>
        <div class="row row-tight">
          <div class="field col-fill"><label class="label" for="retention_minutes">Retention (minute logs), days</label><input class="input input--mono" type="number" id="retention_minutes" name="retention_minutes" value="{{.RetentionMinuteDays}}" min="1" max="30" required></div>
          <div class="field col-fill"><label class="label" for="retention_hourly">Retention (hourly logs), days</label><input class="input input--mono" type="number" id="retention_hourly" name="retention_hourly" value="{{.RetentionHourlyDays}}" min="7" max="365" required></div>
        </div>
        <div class="row row-end row-tight"><button class="btn" data-variant="ghost" type="reset">Reset</button><button class="btn" data-variant="primary" type="submit">Save &amp; restart</button></div>
      </div>
    </section>
  </div>
  <div class="col-5">
    <section class="card">
      <div class="card-head"><h3>Maintenance</h3></div>
      <div class="card-body col col-panel">
        <button class="action-row" type="button" disabled><span class="action-icon">{{icon "Refresh" 14}}</span><span class="summary-copy"><strong>Restart all services</strong><span>teleproxy, tgproxy-panel, nginx-stub</span></span>{{icon "Right" 14}}</button>
        <button class="action-row" type="button" disabled><span class="action-icon">{{icon "Download" 14}}</span><span class="summary-copy"><strong>Download diagnostic bundle</strong><span>Logs, redacted config, versions</span></span>{{icon "Right" 14}}</button>
        <button class="action-row" type="button" disabled><span class="action-icon">{{icon "Upload" 14}}</span><span class="summary-copy"><strong>Restore from backup</strong><span>Replace current config with a snapshot</span></span>{{icon "Right" 14}}</button>
        <button class="action-row danger" type="button" disabled><span class="action-icon">{{icon "Trash" 14}}</span><span class="summary-copy"><strong>Reset to defaults</strong><span>Wipe users, nodes, regenerate keys</span></span>{{icon "Right" 14}}</button>
      </div>
    </section>
    <section class="card">
      <div class="card-head"><h3>Versions</h3></div>
      <div class="card-body card-body--flush">
        <table class="tbl tbl--compact"><tbody><tr><td class="mono">tgproxy-panel</td><td class="mono text-right">installed</td><td class="text-right"><span class="badge" data-tone="success">stable</span></td></tr><tr><td class="mono">teleproxy</td><td class="mono text-right">managed</td><td class="text-right"><span class="badge" data-tone="success">stable</span></td></tr></tbody></table>
      </div>
    </section>
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
