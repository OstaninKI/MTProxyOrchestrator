package panel

import (
	"html/template"
	"io"
)

// proxySettingsData is passed to the proxy settings template.
type proxySettingsData struct {
	CSRFField   string
	CSRFToken   string
	MaskHost    string
	MTProtoPort int
	Success     string // non-empty on successful save
	Error       string // non-empty on validation error
}

// adminPasswordData is passed to the admin password change template.
type adminPasswordData struct {
	CSRFField string
	CSRFToken string
	Success   string // non-empty on successful change
	Error     string // non-empty on validation error
}

// systemSettingsData is passed to the system settings template.
type systemSettingsData struct {
	CSRFField           string
	CSRFToken           string
	PanelPath           string
	LogLevel            string // "debug", "info", "warn", "error"
	RetentionMinuteDays int    // 1-30
	RetentionHourlyDays int    // 7-365
	Success             string // non-empty on successful save
	Error               string // non-empty on validation error
}

var proxySettingsTmpl = template.Must(template.New("proxy_settings").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Proxy Settings</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1{margin-bottom:1rem}
form{max-width:400px}label{display:block;margin-top:1rem;font-weight:bold}
input[type=text],input[type=number]{width:100%;padding:.5rem;margin-top:.25rem;border:1px solid #ccc;border-radius:4px;box-sizing:border-box}
button{margin-top:1.5rem;padding:.5rem 1rem;background:#2563eb;color:#fff;border:none;border-radius:4px;cursor:pointer}
button:hover{background:#1d4ed8}
.success{color:#16a34a;margin-bottom:1rem}.error{color:#dc2626;margin-bottom:1rem}
a{color:#2563eb;margin-right:1rem}</style>
</head>
<body>
<h1>Proxy Settings</h1>
<p><a href="../dashboard">← Dashboard</a> &nbsp;|&nbsp; <a href="admin-password">Admin Password →</a> &nbsp;|&nbsp; <a href="system">System →</a></p>

{{if .Success}}<p class="success">Settings saved successfully.</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<form method="post">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">

<label for="mask_host">Mask host</label>
<input type="text" id="mask_host" name="mask_host" value="{{.MaskHost}}" required>

<label for="mtproto_port">MTProto port</label>
<input type="number" id="mtproto_port" name="mtproto_port" value="{{.MTProtoPort}}" min="1" max="65535" required>

<button type="submit">Save</button>
</form>
</body>
</html>
`))

var adminPasswordTmpl = template.Must(template.New("admin_password").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Change Admin Password</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1{margin-bottom:1rem}
form{max-width:400px}label{display:block;margin-top:1rem;font-weight:bold}
input[type=password]{width:100%;padding:.5rem;margin-top:.25rem;border:1px solid #ccc;border-radius:4px;box-sizing:border-box}
button{margin-top:1.5rem;padding:.5rem 1rem;background:#2563eb;color:#fff;border:none;border-radius:4px;cursor:pointer}
button:hover{background:#1d4ed8}
.success{color:#16a34a;margin-bottom:1rem}.error{color:#dc2626;margin-bottom:1rem}
.hint{font-size:.875rem;color:#555;margin-top:.5rem}
a{color:#2563eb;margin-right:1rem}</style>
</head>
<body>
<h1>Change Admin Password</h1>
<p><a href="../dashboard">← Dashboard</a> &nbsp;|&nbsp; <a href="proxy">Proxy Settings →</a> &nbsp;|&nbsp; <a href="system">System →</a></p>

{{if .Success}}<p class="success">Password changed successfully.</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<form method="post">
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
</body>
</html>
`))

var systemSettingsTmpl = template.Must(template.New("system_settings").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>System Settings</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1{margin-bottom:1rem}
form{max-width:400px}label{display:block;margin-top:1rem;font-weight:bold}
input[type=text],input[type=number],select{width:100%;padding:.5rem;margin-top:.25rem;border:1px solid #ccc;border-radius:4px;box-sizing:border-box}
button{margin-top:1.5rem;padding:.5rem 1rem;background:#2563eb;color:#fff;border:none;border-radius:4px;cursor:pointer}
button:hover{background:#1d4ed8}
.success{color:#16a34a;margin-bottom:1rem}.error{color:#dc2626;margin-bottom:1rem}
.warn-box{background:#fffbeb;border:1px solid #fed7aa;border-radius:6px;padding:1rem;margin-bottom:1.5rem;color:#92400e}
a{color:#2563eb;margin-right:1rem}</style>
</head>
<body>
<h1>System Settings</h1>
<p><a href="../dashboard">← Dashboard</a> &nbsp;|&nbsp; <a href="proxy">Proxy Settings →</a> &nbsp;|&nbsp; <a href="admin-password">Admin Password →</a></p>

{{if .Success}}<p class="success">Settings saved successfully.</p>{{end}}
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}

<div class="warn-box">
<strong>Note:</strong> Panel path and log level changes require restarting tgproxy-panel to take effect.
</div>

<form method="post">
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
</body>
</html>
`))

func proxySettingsPage(w io.Writer, data proxySettingsData) {
	proxySettingsTmpl.Execute(w, data) //nolint:errcheck
}

func adminPasswordPage(w io.Writer, data adminPasswordData) {
	adminPasswordTmpl.Execute(w, data) //nolint:errcheck
}

func systemSettingsPage(w io.Writer, data systemSettingsData) {
	systemSettingsTmpl.Execute(w, data) //nolint:errcheck
}
