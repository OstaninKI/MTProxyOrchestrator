package panel

import (
	"io"
)

const auditContent = `{{define "page_title"}}Audit Log{{end}}
{{define "content"}}
<h1>Audit Log</h1>
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
{{end}}
{{template "base" .}}`

var auditTmpl = layoutTemplate("audit", auditContent, nil)

func auditPage(w io.Writer, panelPath string, entries []auditEntry, csrfToken string) {
	auditTmpl.Execute(w, map[string]any{
		"PanelPath": panelPath,
		"Entries":   entries,
		"CSRFField": CSRFField(),
		"CSRFToken": csrfToken,
	})
}
