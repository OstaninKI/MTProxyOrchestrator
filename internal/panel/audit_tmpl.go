package panel

import (
	"io"
)

const auditContent = `{{define "page_title"}}Audit Log{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Security</p>
    <h1 class="page-title">Audit Log</h1>
    <p class="page-sub">Review administrator actions without exposing raw secrets.</p>
  </div>
  <div class="actions"><a class="btn" data-variant="ghost" href="{{.PanelPath}}settings/system">System settings</a></div>
</section>
<section class="page-stack">
  <section class="grid-12">
    <article class="col-3 card stat-card">
      <div class="card-body"><div class="stat-head"><span class="stat-icon">{{icon "Logs" 15}}</span><span class="stat-label">Entries</span></div><strong class="stat-value mono">{{len .Entries}}</strong><span class="stat-hint">Administrator actions</span></div>
    </article>
    <article class="col-3 card stat-card">
      <div class="card-body"><div class="stat-head"><span class="stat-icon" data-tone="success">{{icon "Shield" 15}}</span><span class="stat-label">Redaction</span></div><strong class="stat-value">On</strong><span class="stat-hint">No raw secrets recorded</span></div>
    </article>
  </section>
  <div class="card table-card">
    <div class="card-body card-body--flush"><table class="tbl">
    <thead>
    <tr>
    <th>ID</th>
    <th>Action</th>
    <th>Target</th>
    <th>Detail</th>
    <th>IP</th>
    <th>Time</th>
    </tr>
    </thead>
    <tbody>
    {{range .Entries}}
    <tr>
    <td class="mono">{{.ID}}</td>
    <td>{{.Action}}</td>
    <td>{{.Target}}</td>
    <td>{{.Detail}}</td>
    <td class="mono">{{.IP}}</td>
    <td class="mono">{{.CreatedAt}}</td>
    </tr>
    {{else}}
    <tr><td colspan="6" class="muted empty-row">No entries.</td></tr>
    {{end}}
    </tbody>
    </table></div>
  </div>
</section>
{{end}}
{{template "base" .}}`

var auditTmpl = layoutTemplate("audit", auditContent, nil)

func auditPage(w io.Writer, panelPath string, entries []auditEntry, csrfToken string) {
	auditTmpl.Execute(w, map[string]any{
		"PanelPath":  panelPath,
		"Entries":    entries,
		"CSRFField":  CSRFField(),
		"CSRFToken":  csrfToken,
		"CurrentNav": "settings",
	})
}
