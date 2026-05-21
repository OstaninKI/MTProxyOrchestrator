package panel

import (
	"html/template"
	"io"
	"strings"
)

type logsPageData struct {
	PanelPath  string
	BasePath   string
	CurrentNav string
	CSRFToken  string
}

var logsTmpl = template.Must(template.New("logs").Funcs(baseLayoutFuncs).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Logs</title>
<link rel="stylesheet" href="{{.BasePath}}/assets/panel.css">
<script defer src="{{.BasePath}}/assets/panel.js"></script>
</head>
<body>
<div class="app" data-logs-page data-panel-path="{{.BasePath}}">
` + sharedTopbar + `
<main class="page">
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Observability</p>
    <h1 class="page-title">Logs</h1>
    <p class="page-sub">Stream recent panel, teleproxy, sing-box, or nginx logs without leaving the admin panel.</p>
  </div>
  <nav class="page-nav" aria-label="Logs navigation">
    <a href="{{.BasePath}}/dashboard">Dashboard</a>
    <a href="{{.BasePath}}/settings/system">System settings</a>
  </nav>
</section>
<section class="page-stack">
<section class="summary-grid logs-kpi-grid">
  <article class="summary-card">
    <span class="summary-label">Buffered</span>
    <strong class="summary-value mono" data-logs-role="count-buffered">0</strong>
    <span class="summary-note">Visible lines in the current stream buffer</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Info</span>
    <strong class="summary-value mono" data-logs-role="count-info">0</strong>
    <span class="summary-note">Informational entries</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Warn</span>
    <strong class="summary-value mono" data-logs-role="count-warn">0</strong>
    <span class="summary-note">Warnings currently buffered</span>
  </article>
  <article class="summary-card">
    <span class="summary-label">Error</span>
    <strong class="summary-value mono" data-logs-role="count-error">0</strong>
    <span class="summary-note">Error entries currently buffered</span>
  </article>
</section>
<div class="card">
<div class="card-body logs-toolbar">
  <div class="logs-toolbar-group logs-toolbar-filters">
    <div class="users-toolbar-group">
      <label>Component</label>
      <select id="component" data-logs-role="component">
        <option value="panel">panel</option>
        <option value="teleproxy">teleproxy</option>
        <option value="sing-box">sing-box</option>
        <option value="nginx">nginx</option>
      </select>
    </div>
    <div class="users-toolbar-group logs-toolbar-search">
      <label>Search</label>
      <input type="text" id="search" class="logs-search" placeholder="filter text..." data-logs-role="search">
    </div>
    <div class="users-toolbar-group">
      <label>Level</label>
      <div class="logs-levels" data-logs-role="level-buttons">
        <button type="button" class="logs-level active" data-level="info">Info</button>
        <button type="button" class="logs-level" data-level="warn">Warn</button>
        <button type="button" class="logs-level" data-level="error">Error</button>
        <button type="button" class="logs-level" data-level="debug">Debug</button>
      </div>
      <select id="level" class="sr-only" data-logs-role="level" aria-label="Log level">
        <option value="debug">debug</option>
        <option value="info" selected>info</option>
        <option value="warn">warn</option>
        <option value="error">error</option>
      </select>
    </div>
  </div>
  <div class="logs-toolbar-group logs-toolbar-actions">
    <button class="btn-pause" id="btnPause" type="button" data-logs-role="pause">Pause</button>
    <button class="btn-clear" id="btnClear" type="button" data-logs-role="clear">Clear</button>
    <button class="logs-level" type="button" data-logs-role="autoscroll">Auto-scroll</button>
    <a class="btn-download" id="btnDownload" href="#" data-logs-role="download">Download (500 lines)</a>
  </div>
</div>
</div>
<div class="card">
<div id="log-container" class="logs-view" data-logs-role="container"></div>
<div class="card-footer logs-footer">
  <div class="logs-footer-copy">
    <div class="logs-status" id="status" data-logs-role="status">Connecting&#8230;</div>
    <span class="logs-footer-note" data-logs-role="summary">Live tail limited to 2000 buffered lines.</span>
  </div>
  <span class="logs-footer-chip" data-logs-role="buffer-chip">0 buffered</span>
</div>
</div>
</section>
</main>
</div>
</body>
</html>
`))

func logsPage(w io.Writer, data logsPageData) {
	data.PanelPath = normalizePanelPath(data.PanelPath)
	if data.BasePath == "" {
		data.BasePath = strings.TrimSuffix(data.PanelPath, "/")
	}
	if data.CurrentNav == "" {
		data.CurrentNav = "logs"
	}
	logsTmpl.Execute(w, data) //nolint:errcheck
}
