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
  <nav class="page-nav" aria-label="Audit navigation">
    <a href="{{.PanelPath}}dashboard">Dashboard</a>
    <a href="{{.PanelPath}}settings/system">System settings</a>
  </nav>
</section>
<div class="card table-card">
<div class="table-wrap"><table>
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
<td>{{.ID}}</td>
<td>{{.Action}}</td>
<td>{{.Target}}</td>
<td>{{.Detail}}</td>
<td>{{.IP}}</td>
<td>{{.CreatedAt}}</td>
</tr>
{{else}}
<tr><td colspan="6" class="muted empty-row">No entries.</td></tr>
{{end}}
</tbody>
</table></div>
</div>
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
