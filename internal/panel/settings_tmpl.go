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
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Stubs</h1>
    <p class="page-sub">Camouflage website served on the same domain.</p>
  </div>
</section>
<section class="page-stack">
<nav class="seg" aria-label="Stub template tabs">
  <a class="seg-item active" href="{{.PanelPath}}settings/stubs">Template library</a>
  <a class="seg-item" href="{{.PanelPath}}settings/stubs/remote">Remote templates</a>
  <a class="seg-item" href="{{.PanelPath}}settings/certificates">Certificates</a>
  <a class="seg-item" href="{{.PanelPath}}settings/proxy">Proxy</a>
</nav>

{{if .ApplySuccess}}<p class="success">Template "{{.ApplySuccess}}" applied successfully.</p>{{end}}
{{if .ApplyError}}<p class="error">{{.ApplyError}}</p>{{end}}
{{if .UploadError}}<p class="error">{{.UploadError}}</p>{{end}}
{{if .UploadValidation}}
<ul class="errors">
{{range .UploadValidation}}<li class="error">{{.}}</li>{{end}}
</ul>
{{end}}

<section class="card">
  <div class="card-body">
    <div class="row row-stretch">
      <div class="stub-preview-lg" aria-hidden="true">S</div>
      <div class="col stub-showcase-copy">
        <span class="badge" data-tone="success"><span class="dot"></span>Currently serving</span>
        <h3 class="hero-title">Built-in stub library</h3>
        <span class="muted">{{len .Templates}} local templates available. Upload ZIP archives up to 5 MB.</span>
        <div class="row stub-showcase-actions">
          <button class="btn" data-variant="primary" disabled>{{icon "Globe" 13}} Preview live</button>
          <button class="btn" data-variant="ghost" disabled>{{icon "Edit" 13}} Edit files</button>
          <button class="btn" data-variant="ghost" disabled>{{icon "Download" 13}} Download as ZIP</button>
        </div>
      </div>
    </div>
  </div>
</section>

<div class="grid-12">
<div class="col-8">
<section class="card">
<div class="card-head"><div class="col card-title-stack"><h3>Template library</h3><span class="sub">Built-in templates bundled with the panel</span></div><div class="spacer"></div><a class="btn" data-size="sm" data-variant="ghost" href="{{.PanelPath}}settings/stubs/remote">{{icon "Refresh" 13}} Sync from GitHub</a></div>
<div class="card-body">
{{if .Templates}}
<div class="template-grid">
{{range .Templates}}
<article class="template-card stub-card">
  <div class="template-card-head">
    <div>
      <h2>{{.Name}}</h2>
      <p class="panel-note">{{.Description}}</p>
    </div>
    <span class="badge ok">built-in</span>
  </div>
  <div class="stub-preview" aria-hidden="true"></div>
  <form method="post" action="stubs/apply" class="template-actions">
    <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
    <input type="hidden" name="template" value="{{.Name}}">
    <button class="btn" data-size="xs" type="submit">Apply</button>
  </form>
</article>
{{end}}
</div>
{{else}}
<p class="muted">No built-in templates available.</p>
{{end}}
</div>
</section>
</div>
<div class="col-4">
<div class="card">
<div class="card-head"><h3>Upload custom template</h3><span class="sub">ZIP up to 5 MB: HTML, CSS, JS, images, fonts</span></div>
<div class="card-body">
<form method="post" action="{{.PanelPath}}settings/stubs/upload" enctype="multipart/form-data" class="stack-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<div class="upload-drop">
  {{icon "Upload" 28}}
  <p>Drop ZIP file here</p>
  <input type="file" name="stub_zip" accept=".zip" required>
</div>
<button class="btn" data-variant="primary" type="submit">Upload and apply</button>
</form>
</div>
</div>
<div class="card">
<div class="card-head"><h3>Stub configuration</h3></div>
<div class="card-body col col-panel">
  <div class="setting-toggle-row"><div class="col setting-toggle-copy"><span class="setting-title">Serve on :80</span><span class="help">HTTP gets a 301 to HTTPS by default.</span></div><span class="toggle"><input type="checkbox" checked disabled><span class="toggle-track"><span class="toggle-thumb"></span></span></span></div>
  <div class="setting-toggle-row"><div class="col setting-toggle-copy"><span class="setting-title">Hide tgproxy headers</span><span class="help">Strip identifying response headers.</span></div><span class="toggle"><input type="checkbox" checked disabled><span class="toggle-track"><span class="toggle-thumb"></span></span></span></div>
  <div class="setting-toggle-row"><div class="col setting-toggle-copy"><span class="setting-title">Cache static assets</span><span class="help">Improves probe authenticity.</span></div><span class="toggle"><input type="checkbox" disabled><span class="toggle-track"><span class="toggle-thumb"></span></span></span></div>
</div>
</div>
</div>
</div>
</section>
{{end}}
{{template "base" .}}`

var settingsStubListTmpl = layoutTemplate("settings_stubs", settingsStubListContent, nil)

const settingsStubRemoteContent = `{{define "page_title"}}Remote Templates{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Stubs</h1>
    <p class="page-sub">Camouflage website served on the same domain.</p>
  </div>
</section>
<section class="page-stack">
<nav class="seg" aria-label="Remote template tabs">
  <a class="seg-item" href="{{.PanelPath}}settings/stubs">Template library</a>
  <a class="seg-item active" href="{{.PanelPath}}settings/stubs/remote">Remote templates</a>
  <a class="seg-item" href="{{.PanelPath}}settings/certificates">Certificates</a>
  <a class="seg-item" href="{{.PanelPath}}settings/proxy">Proxy</a>
</nav>

{{if .ApplySuccess}}<p class="success">Template "{{.ApplySuccess}}" downloaded and applied successfully.</p>{{end}}
{{if .Error}}<div class="warn-box">{{.Error}}</div>{{end}}

<section class="card">
  <div class="card-body">
    <div class="row row-stretch">
      <div class="stub-preview-lg" aria-hidden="true"></div>
      <div class="col stub-showcase-copy">
        <span class="badge" data-tone="accent"><span class="dot"></span>Remote catalog</span>
        <h3 class="hero-title">learning-zone/website-templates</h3>
        <span class="muted">{{len .Templates}} templates fetched for this request. Download and apply runs in one step.</span>
        <div class="row stub-showcase-actions">
          <a class="btn" data-variant="primary" href="{{.PanelPath}}settings/stubs">{{icon "Stubs" 13}} Local library</a>
          <button class="btn" data-variant="ghost" disabled>{{icon "Search" 13}} Search catalog</button>
        </div>
      </div>
    </div>
  </div>
</section>

{{if .Templates}}
<section class="card">
<div class="card-head"><div class="col card-title-stack"><h3>Template library</h3><span class="sub">From GitHub · live preview placeholder until remote previews are implemented</span></div><div class="spacer"></div><a class="btn" data-size="sm" data-variant="ghost" href="https://github.com/learning-zone/website-templates" target="_blank" rel="noopener">{{icon "Globe" 13}} Source</a></div>
<div class="card-body">
<div class="template-grid">
{{range .Templates}}
<article class="template-card stub-card">
  <div class="template-card-head">
    <div>
      <h2>{{.Name}}</h2>
      <p class="panel-note">Fetched and activated in a single step from the remote source.</p>
    </div>
    <span class="badge warn">remote</span>
  </div>
  <div class="stub-preview" aria-hidden="true"></div>
  <form method="post" action="{{$.PanelPath}}settings/stubs/remote-apply" class="template-actions">
    <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
    <input type="hidden" name="template" value="{{.Name}}">
    <button class="btn" data-size="xs" type="submit">Download &amp; Apply</button>
  </form>
</article>
{{end}}
</div>
</div>
</section>
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
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Certificates</h1>
    <p class="page-sub">TLS certificates and ACME renewal.</p>
  </div>
</section>
<section class="page-stack">
<nav class="seg" aria-label="Certificate tabs">
  <a class="seg-item" href="{{.PanelPath}}settings/stubs">Stubs</a>
  <a class="seg-item active" href="{{.PanelPath}}settings/certificates">Certificates</a>
  <a class="seg-item" href="{{.PanelPath}}settings/proxy">Proxy</a>
  <a class="seg-item" href="{{.PanelPath}}settings/system">System</a>
</nav>

{{if not .HasDomain}}
<div class="warn-box">
  <strong>No domain configured.</strong> This server was installed with an IP address only.
  ACME (Let's Encrypt) certificates require a domain name with a valid DNS A record pointing to this server.
  Automatic certificate renewal is unavailable. A self-signed certificate is used.
</div>
{{end}}

<div class="grid-12">
  <div class="col-7">
    <section class="card">
      <div class="card-head">
        <div class="col card-title-stack"><h3>Active certificate</h3><span class="sub">{{if .CertMode}}{{.CertMode}}{{else}}Unknown{{end}} · {{if .HasDomain}}auto-renewing{{else}}manual only{{end}}</span></div>
        <div class="spacer"></div>
        {{if .HasDomain}}<form method="post" action="{{.PanelPath}}settings/certificates/renew" class="inline"><input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}"><button class="btn" data-size="sm" data-variant="primary" type="submit">{{icon "Refresh" 13}} Renew now</button></form>{{else}}<button class="btn" data-size="sm" data-variant="primary" disabled>{{icon "Refresh" 13}} Renew now</button>{{end}}
      </div>
      <div class="card-body">
        <div class="row row-center-wide">
          <div class="ring-lite mono">TLS</div>
          <div class="col col-tight">
            <span class="badge {{if .IsValid}}ok{{else}}down{{end}}"><span class="dot"></span>{{if .IsValid}}Valid · trusted by Telegram clients{{else}}Invalid or missing certificate{{end}}</span>
            {{if not .ExpiresAt.IsZero}}<span class="muted-md">Expires <span class="mono">{{.ExpiresAt.Format "2006-01-02 15:04 UTC"}}</span></span>{{end}}
            {{if not .IssuedAt.IsZero}}<span class="muted-md">Issued <span class="mono">{{.IssuedAt.Format "2006-01-02 15:04 UTC"}}</span></span>{{end}}
          </div>
        </div>
        <div class="cert-pair"><span class="cert-label">Mode</span><span class="cert-value">{{if .CertMode}}{{.CertMode}}{{else}}Unknown{{end}}</span></div>
        {{if .HasDomain}}<div class="cert-pair"><span class="cert-label">Domain</span><span class="cert-value mono">{{.Domain}}</span></div>{{end}}
        {{if .ServerIP}}<div class="cert-pair"><span class="cert-label">Server IP</span><span class="cert-value mono">{{.ServerIP}}</span></div>{{end}}
        {{if .HasDomain}}<div class="cert-pair"><span class="cert-label">SAN</span><span class="cert-value mono">{{.Domain}}, www.{{.Domain}}</span></div>{{end}}
        <div class="cert-pair"><span class="cert-label">Algorithm</span><span class="cert-value mono">ECDSA P-256</span></div>
        <div class="cert-pair"><span class="cert-label">Issuer</span><span class="cert-value mono">{{if .HasDomain}}Let's Encrypt{{else}}Self-signed{{end}}</span></div>
        <div class="cert-pair"><span class="cert-label">Renewal</span><span class="cert-value">{{if and .HasDomain .NeedsRenewal}}Due{{else if .HasDomain}}Healthy{{else}}Unavailable{{end}}</span></div>
      </div>
    </section>
    <section class="card">
      <div class="card-head"><h3>Renewal log</h3></div>
      <div class="card-body card-body--flush">
      {{if .Renewals}}
        {{range .Renewals}}<div class="cert-log-row"><span class="cert-log-ts mono">{{.CreatedAt}}</span><span><strong>{{if .Success}}Issued certificate{{else}}Renewal failed{{end}}</strong> · {{if .ErrorMsg}}{{.ErrorMsg}}{{else}}{{.Domain}}{{end}}</span></div>{{end}}
      {{else}}
        <div class="empty">No renewal attempts recorded.</div>
      {{end}}
      </div>
    </section>
  </div>
  <div class="col-5">
    <section class="card">
      <div class="card-head"><h3>Renewal settings</h3></div>
      <div class="card-body col col-panel">
        <div class="field"><span class="label">Provider</span><select class="select" disabled><option>Let's Encrypt (production)</option></select></div>
        <div class="field"><span class="label">Auto-renew threshold (days)</span><input class="input input--mono" value="30" disabled></div>
        <div class="setting-toggle-row"><div class="col setting-toggle-copy"><span class="setting-title">Auto-renew enabled</span><span class="help">Renews via cron when threshold reached.</span></div><span class="toggle"><input type="checkbox" checked disabled><span class="toggle-track"><span class="toggle-thumb"></span></span></span></div>
        <div class="setting-toggle-row"><div class="col setting-toggle-copy"><span class="setting-title">Notify on renewal</span><span class="help">Email and Telegram notifications are not implemented yet.</span></div><span class="toggle"><input type="checkbox" checked disabled><span class="toggle-track"><span class="toggle-thumb"></span></span></span></div>
        <button class="btn" data-variant="primary" disabled>{{icon "Check" 12}} Save settings</button>
      </div>
    </section>
    <section class="card">
      <div class="card-head"><h3>Manual certificate</h3></div>
      <div class="card-body">
        <p class="help">Upload your own fullchain.pem + privkey.pem to override ACME.</p>
        <div class="row row-tight row-wrap">
          <button class="btn" data-size="sm" data-variant="ghost" disabled>{{icon "Upload" 12}} Upload chain</button>
          <button class="btn" data-size="sm" data-variant="ghost" disabled>{{icon "Upload" 12}} Upload key</button>
        </div>
        <div class="cert-pair"><span class="cert-label">Override state</span><span class="cert-value">Not configured</span></div>
      </div>
    </section>
  </div>
</div>
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
