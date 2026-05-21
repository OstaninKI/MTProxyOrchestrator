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
	CurrentNav       string
	PanelPath        string
}

type settingsStubRemoteData struct {
	CSRFField    string
	CSRFToken    string
	Templates    []RemoteStubTemplate
	ApplySuccess string
	Error        string
	CurrentNav   string
	PanelPath    string
}

type settingsCertData struct {
	CSRFField    string
	CSRFToken    string
	HasDomain    bool
	Domain       string
	ServerIP     string
	CertMode     string
	ExpiresAt    time.Time
	IssuedAt     time.Time
	IsValid      bool
	NeedsRenewal bool
	Renewals     []RenewalAttempt
	CurrentNav   string
	PanelPath    string
}

const settingsStubListContent = `{{define "page_title"}}Stub Templates{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Settings</p>
    <h1 class="page-title">Stub Templates</h1>
    <p class="page-sub">Manage the public fallback page templates that are served when the proxy front is reached.</p>
  </div>
</section>
<section class="page-stack">
<nav class="settings-tabs" aria-label="Stub template tabs">
  <a class="active" href="{{.PanelPath}}settings/stubs">Stub templates</a>
  <a href="{{.PanelPath}}settings/stubs/remote">Remote templates</a>
  <a href="{{.PanelPath}}settings/certificates">Certificates</a>
  <a href="{{.PanelPath}}settings/proxy">Proxy</a>
</nav>

{{if .ApplySuccess}}<p class="success">Template "{{.ApplySuccess}}" applied successfully.</p>{{end}}
{{if .ApplyError}}<p class="error">{{.ApplyError}}</p>{{end}}
{{if .UploadError}}<p class="error">{{.UploadError}}</p>{{end}}
{{if .UploadValidation}}
<ul class="errors">
{{range .UploadValidation}}<li class="error">{{.}}</li>{{end}}
</ul>
{{end}}

<section class="summary-grid">
  <article class="summary-card">
    <span class="summary-label">Built-in</span>
    <strong class="summary-value mono">{{len .Templates}}</strong>
    <span class="summary-note">Templates bundled with the panel</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Upload limit</span>
    <strong class="summary-value mono">5 MB</strong>
    <span class="summary-note">ZIP archives only</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Allowed assets</span>
    <strong class="summary-value">HTML/CSS/JS</strong>
    <span class="summary-note">Images and fonts are accepted too</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Remote catalog</span>
    <strong class="summary-value">GitHub</strong>
    <span class="summary-note">Browse more templates from the remote tab</span>
  </article>
</section>

{{if .Templates}}
<div class="template-grid">
{{range .Templates}}
<article class="card template-card">
  <div class="template-card-head">
    <div>
      <h2>{{.Name}}</h2>
      <p class="panel-note">{{.Description}}</p>
    </div>
    <span class="badge ok">built-in</span>
  </div>
  <div class="template-preview" aria-hidden="true">{{printf "%.1s" .Name}}</div>
  <form method="post" action="stubs/apply" class="template-actions">
    <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
    <input type="hidden" name="template" value="{{.Name}}">
    <button type="submit">Apply</button>
  </form>
</article>
{{end}}
</div>
{{else}}
<p class="muted">No built-in templates available.</p>
{{end}}

<div class="stack-split">
<div class="card form-panel">
<h2>Upload Custom Template</h2>
<p class="panel-note">Upload a ZIP archive up to 5 MB. Allowed files: HTML, CSS, JS, images, and fonts.</p>
<form method="post" action="{{.PanelPath}}settings/stubs/upload" enctype="multipart/form-data" class="stack-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<input type="file" name="stub_zip" accept=".zip" required>
<button type="submit">Upload and apply</button>
</form>
</div>
<aside class="card side-panel">
<h2>Template Notes</h2>
<div class="summary-list">
  <div class="summary-row">
    <span class="badge">Preview</span>
    <span class="summary-copy"><strong>Local bundle</strong><span>Uploaded archives are validated before activation.</span></span>
  </div>
  <div class="summary-row">
    <span class="badge warn">Fallback</span>
    <span class="summary-copy"><strong>Remote catalog</strong><span>Use the remote tab when built-ins are too limited.</span></span>
  </div>
  <div class="summary-row">
    <span class="badge ok">Scope</span>
    <span class="summary-copy"><strong>Public stub only</strong><span>These templates do not affect the authenticated panel shell.</span></span>
  </div>
</div>
</aside>
</div>
<nav class="page-nav" aria-label="Stub template links">
  <a href="{{.PanelPath}}settings/stubs/remote">Browse remote templates</a>
  <a href="{{.PanelPath}}settings/certificates">Certificate settings</a>
</nav>
</section>
{{end}}
{{template "base" .}}`

var settingsStubListTmpl = layoutTemplate("settings_stubs", settingsStubListContent, nil)

const settingsStubRemoteContent = `{{define "page_title"}}Remote Templates{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Settings</p>
    <h1 class="page-title">Remote Templates</h1>
    <p class="page-sub">Templates are fetched from GitHub and then applied as the active public stub page.</p>
  </div>
</section>
<section class="page-stack">
<nav class="settings-tabs" aria-label="Remote template tabs">
  <a href="{{.PanelPath}}settings/stubs">Stub templates</a>
  <a class="active" href="{{.PanelPath}}settings/stubs/remote">Remote templates</a>
  <a href="{{.PanelPath}}settings/certificates">Certificates</a>
  <a href="{{.PanelPath}}settings/proxy">Proxy</a>
</nav>
<p class="panel-note">Source: <a href="https://github.com/learning-zone/website-templates" target="_blank" rel="noopener">learning-zone/website-templates</a>.</p>

{{if .ApplySuccess}}<p class="success">Template "{{.ApplySuccess}}" downloaded and applied successfully.</p>{{end}}
{{if .Error}}<div class="warn-box">{{.Error}}</div>{{end}}

<section class="summary-grid">
  <article class="summary-card">
    <span class="summary-label">Remote source</span>
    <strong class="summary-value">GitHub</strong>
    <span class="summary-note">learning-zone/website-templates</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Available</span>
    <strong class="summary-value mono">{{len .Templates}}</strong>
    <span class="summary-note">Templates fetched for this request</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Flow</span>
    <strong class="summary-value">Download</strong>
    <span class="summary-note">Fetched and applied in one step</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Fallback</span>
    <strong class="summary-value">Built-ins</strong>
    <span class="summary-note">Switch back to local templates at any time</span>
  </article>
</section>

{{if .Templates}}
<div class="template-grid">
{{range .Templates}}
<article class="card template-card">
  <div class="template-card-head">
    <div>
      <h2>{{.Name}}</h2>
      <p class="panel-note">Fetched and activated in a single step from the remote source.</p>
    </div>
    <span class="badge warn">remote</span>
  </div>
  <div class="template-preview" aria-hidden="true">{{printf "%.1s" .Name}}</div>
  <form method="post" action="{{$.PanelPath}}settings/stubs/remote-apply" class="template-actions">
    <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
    <input type="hidden" name="template" value="{{.Name}}">
    <button type="submit">Download &amp; Apply</button>
  </form>
</article>
{{end}}
</div>
{{else if not .Error}}
<p class="muted">No templates available.</p>
{{end}}
</section>
{{end}}
{{template "base" .}}`

var settingsStubRemoteTmpl = layoutTemplate("settings_stubs_remote", settingsStubRemoteContent, nil)

func settingsStubRemotePage(w io.Writer, data settingsStubRemoteData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "stubs"
	}
	settingsStubRemoteTmpl.Execute(w, data) //nolint:errcheck
}

const settingsCertContent = `{{define "page_title"}}Certificates{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Settings</p>
    <h1 class="page-title">Certificates</h1>
    <p class="page-sub">Review TLS certificate state, validity window, and recent renewal attempts.</p>
  </div>
</section>
<section class="page-stack">
<nav class="settings-tabs" aria-label="Certificate tabs">
  <a href="{{.PanelPath}}settings/stubs">Stub templates</a>
  <a class="active" href="{{.PanelPath}}settings/certificates">Certificates</a>
  <a href="{{.PanelPath}}settings/proxy">Proxy</a>
  <a href="{{.PanelPath}}settings/system">System</a>
</nav>

{{if not .HasDomain}}
<div class="warn-box">
  <strong>No domain configured.</strong> This server was installed with an IP address only.
  ACME (Let's Encrypt) certificates require a domain name with a valid DNS A record pointing to this server.
  Automatic certificate renewal is unavailable. A self-signed certificate is used.
</div>
{{end}}

<section class="summary-grid">
  <article class="summary-card">
    <span class="summary-label">Mode</span>
    <strong class="summary-value">{{if .CertMode}}{{.CertMode}}{{else}}Unknown{{end}}</strong>
    <span class="summary-note">{{if .HasDomain}}{{.Domain}}{{else}}IP-only installation{{end}}</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Validity</span>
    <strong class="summary-value">{{if .IsValid}}Valid{{else}}Invalid{{end}}</strong>
    <span class="summary-note">{{if not .ExpiresAt.IsZero}}Expires {{.ExpiresAt.Format "2006-01-02"}}{{else}}No active expiry data{{end}}</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Renewal</span>
    <strong class="summary-value">{{if and .HasDomain .NeedsRenewal}}Due{{else if .HasDomain}}Healthy{{else}}Unavailable{{end}}</strong>
    <span class="summary-note">{{if and .HasDomain .NeedsRenewal}}Expires within 30 days{{else if .HasDomain}}Automatic renewals can run{{else}}Requires a domain name{{end}}</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Attempts</span>
    <strong class="summary-value mono">{{len .Renewals}}</strong>
    <span class="summary-note">Recent renewal records stored in panel DB</span>
  </article>
</section>

<div class="stack-split">
<section class="card">
<div class="card-body">
<h2>Current certificate</h2>
<p class="panel-note">This is the best-effort view of the certificate currently used by the panel edge.</p>
</div>
<div class="summary-list cert-details">
  <div class="summary-row"><span class="badge">Mode</span><span class="summary-copy"><strong>{{if .CertMode}}{{.CertMode}}{{else}}Unknown{{end}}</strong><span>Issuance mode currently detected by the panel</span></span></div>
  {{if .HasDomain}}<div class="summary-row"><span class="badge ok">Domain</span><span class="summary-copy"><strong class="mono">{{.Domain}}</strong><span>Configured DNS name for ACME and TLS</span></span></div>{{end}}
  {{if .ServerIP}}<div class="summary-row"><span class="badge">Server IP</span><span class="summary-copy"><strong class="mono">{{.ServerIP}}</strong><span>Public endpoint currently associated with this install</span></span></div>{{end}}
  {{if not .ExpiresAt.IsZero}}<div class="summary-row"><span class="badge warn">Expires</span><span class="summary-copy"><strong>{{.ExpiresAt.Format "2006-01-02 15:04 UTC"}}</strong><span>Renew before the validity window closes</span></span></div>{{end}}
  {{if not .IssuedAt.IsZero}}<div class="summary-row"><span class="badge">Issued</span><span class="summary-copy"><strong>{{.IssuedAt.Format "2006-01-02 15:04 UTC"}}</strong><span>Best-effort issuance timestamp from local certificate state</span></span></div>{{end}}
  <div class="summary-row"><span class="badge {{if .IsValid}}ok{{else}}down{{end}}">Valid</span><span class="summary-copy"><strong>{{if .IsValid}}Yes{{else}}No{{end}}</strong><span>{{if and .HasDomain .NeedsRenewal}}Renewal is due within 30 days{{else if .IsValid}}Certificate is currently usable{{else}}Certificate state needs attention{{end}}</span></span></div>
</div>
</section>
<aside class="card side-panel">
<h2>Renewal Summary</h2>
<div class="summary-list">
  <div class="summary-row">
    <span class="badge {{if and .HasDomain .NeedsRenewal}}warn{{else if .HasDomain}}ok{{else}}down{{end}}">State</span>
    <span class="summary-copy"><strong>{{if and .HasDomain .NeedsRenewal}}Due{{else if .HasDomain}}Healthy{{else}}Unavailable{{end}}</strong><span>{{if and .HasDomain .NeedsRenewal}}Expiry is approaching{{else if .HasDomain}}Automatic renewal path is available{{else}}ACME requires a domain name{{end}}</span></span>
  </div>
  <div class="summary-row">
    <span class="badge">Attempts</span>
    <span class="summary-copy"><strong class="mono">{{len .Renewals}}</strong><span>Recent renewal records stored in the panel database</span></span>
  </div>
</div>
</aside>
</div>

{{if .Renewals}}
<section class="card">
<div class="card-body">
<h2>Recent renewal attempts</h2>
<p class="panel-note">Operational history for ACME renewals and certificate refresh attempts.</p>
</div>
<div class="summary-list renewal-feed">
{{range .Renewals}}
  <div class="summary-row">
    <span class="badge {{if .Success}}ok{{else}}down{{end}}">{{if .Success}}success{{else}}failed{{end}}</span>
    <span class="summary-copy">
      <strong>{{.Domain}}</strong>
      <span>{{if .ErrorMsg}}{{.ErrorMsg}}{{else}}Certificate renewal completed without stored error text{{end}}</span>
      <span class="mono">{{.CreatedAt}}</span>
    </span>
  </div>
{{end}}
</div>
</section>
{{end}}
</section>
{{end}}
{{template "base" .}}`

var settingsCertTmpl = layoutTemplate("settings_certs", settingsCertContent, nil)

func settingsStubListPage(w io.Writer, data settingsStubListData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "stubs"
	}
	settingsStubListTmpl.Execute(w, data)
}

func settingsCertPage(w io.Writer, data settingsCertData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "certificates"
	}
	settingsCertTmpl.Execute(w, data)
}
