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
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Logs</h1>
    <p class="page-sub">Live event stream from all panel services.</p>
  </div>
</section>
<section class="page-stack">
<section class="grid-12 logs-kpi-grid">
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon">{{icon "Activity" 15}}</span><span class="stat-label">Lines / 5m</span></div><strong class="stat-value mono" data-logs-role="count-buffered">0</strong></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon">{{icon "Info" 15}}</span><span class="stat-label">Info</span></div><strong class="stat-value mono" data-logs-role="count-info">0</strong></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon" data-tone="warn">{{icon "Bell" 15}}</span><span class="stat-label">Warnings</span></div><strong class="stat-value mono" data-logs-role="count-warn">0</strong></div>
  </article>
  <article class="col-3 card stat-card">
    <div class="card-body"><div class="stat-head"><span class="stat-icon" data-tone="danger">{{icon "X" 15}}</span><span class="stat-label">Errors</span></div><strong class="stat-value mono" data-logs-role="count-error">0</strong></div>
  </article>
</section>
<div class="card">
<div class="card-body logs-toolbar">
  <div class="logs-toolbar-group logs-toolbar-filters">
    <div class="row row-tight" data-logs-role="level-buttons">
      <span class="label">Levels</span>
      <button type="button" class="badge logs-level" data-level="debug">debug</button>
      <button type="button" class="badge logs-level active" data-level="info"><span class="dot"></span>info</button>
      <button type="button" class="badge logs-level" data-tone="warn" data-level="warn"><span class="dot"></span>warn</button>
      <button type="button" class="badge logs-level" data-tone="danger" data-level="error"><span class="dot"></span>error</button>
    </div>
    <div class="users-toolbar-group">
      <label class="label">Source</label>
      <select class="select" id="component" data-logs-role="component">
        <option value="panel">panel</option>
        <option value="teleproxy">teleproxy</option>
        <option value="sing-box">sing-box</option>
        <option value="nginx">nginx</option>
      </select>
    </div>
    <div class="users-toolbar-group logs-toolbar-search">
      <label class="label">Search</label>
      <div class="input-group">{{icon "Search" 13}}<input type="text" id="search" class="input logs-search" placeholder="Filter messages..." data-logs-role="search"></div>
    </div>
    <div class="users-toolbar-group">
      <label class="label">Level</label>
      <select id="level" class="sr-only" data-logs-role="level" aria-label="Log level">
        <option value="debug">debug</option>
        <option value="info" selected>info</option>
        <option value="warn">warn</option>
        <option value="error">error</option>
      </select>
    </div>
  </div>
  <div class="logs-toolbar-group logs-toolbar-actions">
    <label class="row logs-autoscroll"><span class="toggle"><input type="checkbox" checked disabled><span class="toggle-track"><span class="toggle-thumb"></span></span></span> Auto-scroll</label>
    <button class="btn btn-pause" data-size="sm" data-variant="ghost" id="btnPause" type="button" data-logs-role="pause">{{icon "Pause" 12}} Pause</button>
    <button class="btn btn-clear" data-size="sm" data-variant="ghost" id="btnClear" type="button" data-logs-role="clear">Clear</button>
    <button class="btn logs-level" data-size="sm" data-variant="ghost" type="button" data-logs-role="autoscroll">Auto-scroll</button>
    <a class="btn btn-download" data-size="sm" data-variant="ghost" id="btnDownload" href="#" data-logs-role="download">{{icon "Download" 13}} Download</a>
  </div>
</div>
</div>
<div class="card">
<div id="log-container" class="logs-view" data-logs-role="container"></div>
<div class="card-footer logs-footer">
  <div class="logs-footer-copy">
    <div class="logs-status" id="status" data-logs-role="status">Connecting&#8230;</div>
    <span class="logs-footer-note" data-logs-role="summary"><span class="pulse-dot pulse-dot--inline"></span>Streaming live tail limited to 2000 buffered lines.</span>
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
