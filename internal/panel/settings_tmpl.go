package panel

import (
	"io"
	"time"
)

type settingsStubListData struct {
	CSRFField        string
	CSRFToken        string
	Templates        []BuiltinStubTemplate
	ApplySuccess     string
	ApplyError       string
	UploadError      string
	UploadValidation []string
	PanelPath        string
}

type settingsStubRemoteData struct {
	CSRFField    string
	CSRFToken    string
	Templates    []RemoteStubTemplate
	ApplySuccess string
	Error        string
	PanelPath    string
}

type settingsCertData struct {
	HasDomain    bool
	Domain       string
	ServerIP     string
	CertMode     string
	ExpiresAt    time.Time
	IssuedAt     time.Time
	IsValid      bool
	NeedsRenewal bool
	Renewals     []RenewalAttempt
	PanelPath    string
}

const settingsStubListContent = `{{define "page_title"}}Stub Templates{{end}}
{{define "content"}}
<h1>Stub Templates</h1>
<p><a href="../dashboard">← Dashboard</a></p>

{{if .ApplySuccess}}<p class="success">Template "{{.ApplySuccess}}" applied successfully.</p>{{end}}
{{if .ApplyError}}<p class="error">{{.ApplyError}}</p>{{end}}
{{if .UploadError}}<p class="error">{{.UploadError}}</p>{{end}}
{{if .UploadValidation}}
<ul class="errors">
{{range .UploadValidation}}<li class="error">{{.}}</li>{{end}}
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
    <form method="post" action="stubs/apply" class="inline">
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
<p class="muted">No built-in templates available.</p>
{{end}}

<h2>Upload custom template</h2>
<p style="font-size:.875rem;color:var(--muted)">Upload a ZIP archive (max 5 MB). Allowed files: HTML, CSS, JS, images, fonts.</p>
<form method="post" action="stubs/upload" enctype="multipart/form-data">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<input type="file" name="stub_zip" accept=".zip" required><br>
<button type="submit">Upload and apply</button>
</form>

<p style="margin-top:2rem">
  <a href="stubs/remote">Browse remote templates (GitHub) →</a>
  &nbsp;|&nbsp;
  <a href="../settings/certificates">Certificate settings →</a>
</p>
{{end}}
{{template "base" .}}`

var settingsStubListTmpl = layoutTemplate("settings_stubs", settingsStubListContent, nil)

const settingsStubRemoteContent = `{{define "page_title"}}Remote Templates{{end}}
{{define "content"}}
<h1>Remote Templates</h1>
<p><a href="../stubs">← Stub Templates</a></p>
<p style="font-size:.875rem;color:var(--muted)">Templates from <a href="https://github.com/learning-zone/website-templates" target="_blank" rel="noopener">learning-zone/website-templates</a>. Files are downloaded from GitHub and applied as the stub page.</p>

{{if .ApplySuccess}}<p class="success">Template "{{.ApplySuccess}}" downloaded and applied successfully.</p>{{end}}
{{if .Error}}<div class="warn-box">{{.Error}}</div>{{end}}

{{if .Templates}}
<div class="table-wrap" style="margin-top:1rem"><table>
<thead><tr><th>Template</th><th>Action</th></tr></thead>
<tbody>
{{range .Templates}}
<tr>
  <td>{{.Name}}</td>
  <td>
    <form method="post" action="remote-apply" class="inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <input type="hidden" name="template" value="{{.Name}}">
      <button type="submit">Download &amp; Apply</button>
    </form>
  </td>
</tr>
{{end}}
</tbody>
</table></div>
{{else if not .Error}}
<p class="muted">No templates available.</p>
{{end}}
{{end}}
{{template "base" .}}`

var settingsStubRemoteTmpl = layoutTemplate("settings_stubs_remote", settingsStubRemoteContent, nil)

func settingsStubRemotePage(w io.Writer, data settingsStubRemoteData) {
	settingsStubRemoteTmpl.Execute(w, data) //nolint:errcheck
}

const settingsCertContent = `{{define "page_title"}}Certificates{{end}}
{{define "content"}}
<h1>Certificate Settings</h1>
<p><a href="../dashboard">← Dashboard</a> &nbsp;|&nbsp; <a href="stubs">Stub templates →</a></p>

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
  <td>{{if .IsValid}}<span class="ok">Yes</span>{{else}}<span class="error" style="margin:0">No</span>{{end}}</td>
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
  <td>{{if .Success}}<span class="ok">success</span>{{else}}<span class="error" style="margin:0">failed</span>{{end}}</td>
  <td>{{.ErrorMsg}}</td>
  <td>{{.CreatedAt}}</td>
</tr>
{{end}}
</tbody>
</table>
{{end}}
{{end}}
{{template "base" .}}`

var settingsCertTmpl = layoutTemplate("settings_certs", settingsCertContent, nil)

func settingsStubListPage(w io.Writer, data settingsStubListData) {
	settingsStubListTmpl.Execute(w, data)
}

func settingsCertPage(w io.Writer, data settingsCertData) {
	settingsCertTmpl.Execute(w, data)
}
