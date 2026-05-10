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
	PanelPath   string
}

type adminPasswordData struct {
	CSRFField string
	CSRFToken string
	Success   string
	Error     string
	PanelPath string
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
}

const proxySettingsContent = `{{define "page_title"}}Proxy Settings{{end}}
{{define "content"}}
<h1>Proxy Settings</h1>
<p><a href="../dashboard">← Dashboard</a> &nbsp;|&nbsp; <a href="admin-password">Admin Password →</a> &nbsp;|&nbsp; <a href="system">System →</a></p>

{{if .Success}}<p class="success">{{.Success}}</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<form method="post" style="max-width:400px">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">

<label for="mask_host">Mask host</label>
<input type="text" id="mask_host" name="mask_host" value="{{.MaskHost}}" required>

<label for="mtproto_port">MTProto port</label>
<input type="number" id="mtproto_port" name="mtproto_port" value="{{.MTProtoPort}}" min="1" max="65535" required>

<label for="server_addr">Server IP or domain (used in proxy links)</label>
<input type="text" id="server_addr" name="server_addr" value="{{.ServerAddr}}" placeholder="e.g. 1.2.3.4 or proxy.example.com">

<button type="submit">Save</button>
</form>
{{end}}
{{template "base" .}}`

var proxySettingsTmpl = layoutTemplate("proxy_settings", proxySettingsContent, nil)

const adminPasswordContent = `{{define "page_title"}}Change Admin Password{{end}}
{{define "content"}}
<style>.hint{font-size:.875rem;color:var(--muted);margin-top:.5rem}</style>
<h1>Change Admin Password</h1>
<p><a href="../dashboard">← Dashboard</a> &nbsp;|&nbsp; <a href="proxy">Proxy Settings →</a> &nbsp;|&nbsp; <a href="system">System →</a></p>

{{if .Success}}<p class="success">{{.Success}}</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<form method="post" style="max-width:400px">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">

<label for="current_password">Current password</label>
<input type="password" id="current_password" name="current_password" required>

<label for="new_password">New password</label>
<input type="password" id="new_password" name="new_password" required>
<p class="hint">Minimum 16 characters, must contain letters and digits.</p>

<label for="confirm_password">Confirm password</label>
<input type="password" id="confirm_password" name="confirm_password" required>

<button type="submit">Change Password</button>
</form>
{{end}}
{{template "base" .}}`

var adminPasswordTmpl = layoutTemplate("admin_password", adminPasswordContent, nil)

const systemSettingsContent = `{{define "page_title"}}System Settings{{end}}
{{define "content"}}
<h1>System Settings</h1>
<p><a href="../dashboard">← Dashboard</a> &nbsp;|&nbsp; <a href="proxy">Proxy Settings →</a> &nbsp;|&nbsp; <a href="admin-password">Admin Password →</a></p>

{{if .Success}}<p class="success">{{.Success}}</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<div class="warn-box">
<strong>Note:</strong> Panel path and log level changes require restarting tgproxy-panel to take effect.
</div>

<form method="post" style="max-width:400px">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">

<label for="panel_path">Panel path</label>
<input type="text" id="panel_path" name="panel_path" value="{{.PanelPath}}" required>

<label for="log_level">Log level</label>
<select id="log_level" name="log_level" required>
<option value="debug" {{if eq .LogLevel "debug"}}selected{{end}}>debug</option>
<option value="info" {{if eq .LogLevel "info"}}selected{{end}}>info</option>
<option value="warn" {{if eq .LogLevel "warn"}}selected{{end}}>warn</option>
<option value="error" {{if eq .LogLevel "error"}}selected{{end}}>error</option>
</select>

<label for="retention_minutes">Log retention (minutes): days</label>
<input type="number" id="retention_minutes" name="retention_minutes" value="{{.RetentionMinuteDays}}" min="1" max="30" required>

<label for="retention_hourly">Log retention (hourly): days</label>
<input type="number" id="retention_hourly" name="retention_hourly" value="{{.RetentionHourlyDays}}" min="7" max="365" required>

<button type="submit">Save</button>
</form>
{{end}}
{{template "base" .}}`

var systemSettingsTmpl = layoutTemplate("system_settings", systemSettingsContent, nil)

func proxySettingsPage(w io.Writer, data proxySettingsData) {
	proxySettingsTmpl.Execute(w, data)
}

func adminPasswordPage(w io.Writer, data adminPasswordData) {
	adminPasswordTmpl.Execute(w, data)
}

func systemSettingsPage(w io.Writer, data systemSettingsData) {
	systemSettingsTmpl.Execute(w, data)
}
