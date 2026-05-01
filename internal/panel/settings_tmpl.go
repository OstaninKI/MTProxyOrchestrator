package panel

import (
	"html/template"
	"io"
	"time"
)

// settingsStubListData is passed to the stub list/upload template.
type settingsStubListData struct {
	CSRFField        string
	CSRFToken        string
	Templates        []BuiltinStubTemplate
	ApplySuccess     string   // non-empty on successful apply
	ApplyError       string   // non-empty on rollback error
	UploadError      string   // non-empty on upload-level error
	UploadValidation []string // non-empty on ZIP validation errors
}

// settingsCertData is passed to the certificate state template.
type settingsCertData struct {
	HasDomain    bool
	Domain       string
	ServerIP     string
	CertMode     string // "ACME (Let's Encrypt)", "Self-signed", "none", or ""
	ExpiresAt    time.Time
	IssuedAt     time.Time
	IsValid      bool
	NeedsRenewal bool
	Renewals     []RenewalAttempt
}

var settingsStubListTmpl = template.Must(template.New("settings_stubs").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Stub Templates</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1,h2{margin-bottom:1rem}a{color:#2563eb}
table{border-collapse:collapse;width:100%;margin-bottom:2rem}th,td{text-align:left;padding:.5rem;border-bottom:1px solid #e5e7eb}
button{padding:.4rem .8rem;background:#2563eb;color:#fff;border:none;border-radius:4px;cursor:pointer}
button:hover{background:#1d4ed8}
.success{color:#16a34a;margin-bottom:1rem}.error{color:#dc2626;margin-bottom:1rem}
ul.errors{margin:.5rem 0 1rem;padding-left:1.5rem;color:#dc2626}
input[type=file]{margin-bottom:.75rem}</style>
</head>
<body>
<h1>Stub Templates</h1>
<p><a href="../dashboard">← Dashboard</a></p>

{{if .ApplySuccess}}<p class="success">Template "{{.ApplySuccess}}" applied successfully.</p>{{end}}
{{if .ApplyError}}<p class="error">{{.ApplyError}}</p>{{end}}
{{if .UploadError}}<p class="error">{{.UploadError}}</p>{{end}}
{{if .UploadValidation}}
<ul class="errors">
{{range .UploadValidation}}<li>{{.}}</li>{{end}}
</ul>
{{end}}

{{if .Templates}}
<h2>Built-in templates</h2>
<table>
<thead><tr><th>Name</th><th>Description</th><th>Action</th></tr></thead>
<tbody>
{{range .Templates}}
<tr>
  <td>{{.Name}}</td>
  <td>{{.Description}}</td>
  <td>
    <form method="post" action="stubs/apply">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <input type="hidden" name="template" value="{{.Name}}">
      <button type="submit">Apply</button>
    </form>
  </td>
</tr>
{{end}}
</tbody>
</table>
{{else}}
<p style="color:#888">No built-in templates available.</p>
{{end}}

<h2>Upload custom template</h2>
<p style="font-size:.875rem;color:#555">Upload a ZIP archive (max 5 MB). Allowed files: HTML, CSS, JS, images, fonts.</p>
<form method="post" action="stubs/upload" enctype="multipart/form-data">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<input type="file" name="stub_zip" accept=".zip" required><br>
<button type="submit">Upload and apply</button>
</form>

<p style="margin-top:2rem"><a href="../settings/certificates">Certificate settings →</a></p>
</body>
</html>
`))

var settingsCertTmpl = template.Must(template.New("settings_certs").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Certificates</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1,h2{margin-bottom:1rem}a{color:#2563eb}
table{border-collapse:collapse;width:100%;margin-bottom:2rem}th,td{text-align:left;padding:.5rem;border-bottom:1px solid #e5e7eb}
.ok{color:#16a34a}.warn{color:#d97706}.err{color:#dc2626}
.info-box{background:#f0f9ff;border:1px solid #bae6fd;border-radius:6px;padding:1rem;margin-bottom:1.5rem}
.warn-box{background:#fffbeb;border:1px solid #fed7aa;border-radius:6px;padding:1rem;margin-bottom:1.5rem}</style>
</head>
<body>
<h1>Certificate Settings</h1>
<p><a href="../../dashboard">← Dashboard</a> &nbsp;|&nbsp; <a href="../settings/stubs">Stub templates →</a></p>

{{if not .HasDomain}}
<div class="warn-box">
  <strong>No domain configured.</strong> This server was installed with an IP address only.
  ACME (Let's Encrypt) certificates require a domain name with a valid DNS A record pointing to this server.
  Automatic certificate renewal is unavailable. A self-signed certificate is used.
</div>
{{end}}

<h2>Current certificate</h2>
<table>
<thead><tr><th>Property</th><th>Value</th></tr></thead>
<tbody>
<tr><td>Mode</td><td>{{if .CertMode}}{{.CertMode}}{{else}}Unknown{{end}}</td></tr>
{{if .HasDomain}}<tr><td>Domain</td><td>{{.Domain}}</td></tr>{{end}}
{{if .ServerIP}}<tr><td>Server IP</td><td>{{.ServerIP}}</td></tr>{{end}}
{{if not .ExpiresAt.IsZero}}<tr><td>Expires</td><td>{{.ExpiresAt.Format "2006-01-02 15:04 UTC"}}</td></tr>{{end}}
{{if not .IssuedAt.IsZero}}<tr><td>Issued</td><td>{{.IssuedAt.Format "2006-01-02 15:04 UTC"}}</td></tr>{{end}}
<tr>
  <td>Valid</td>
  <td>{{if .IsValid}}<span class="ok">Yes</span>{{else}}<span class="err">No</span>{{end}}</td>
</tr>
{{if and .HasDomain .NeedsRenewal}}
<tr>
  <td>Renewal</td>
  <td><span class="warn">Due (expires within 30 days)</span></td>
</tr>
{{end}}
</tbody>
</table>

{{if .Renewals}}
<h2>Recent renewal attempts</h2>
<table>
<thead><tr><th>Domain</th><th>Result</th><th>Error</th><th>Time</th></tr></thead>
<tbody>
{{range .Renewals}}
<tr>
  <td>{{.Domain}}</td>
  <td>{{if .Success}}<span class="ok">success</span>{{else}}<span class="err">failed</span>{{end}}</td>
  <td>{{.ErrorMsg}}</td>
  <td>{{.CreatedAt}}</td>
</tr>
{{end}}
</tbody>
</table>
{{end}}
</body>
</html>
`))

func settingsStubListPage(w io.Writer, data settingsStubListData) {
	settingsStubListTmpl.Execute(w, data) //nolint:errcheck
}

func settingsCertPage(w io.Writer, data settingsCertData) {
	settingsCertTmpl.Execute(w, data) //nolint:errcheck
}
