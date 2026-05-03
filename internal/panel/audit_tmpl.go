package panel

import (
	"html/template"
	"io"
)

var auditTmpl = template.Must(template.New("audit").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Audit Log</title>
<style>
*{box-sizing:border-box}
body{font-family:sans-serif;margin:0;background:#f5f5f5;padding:1rem}
h1{margin-top:0}
.back-link{font-size:.9rem;color:#0066cc;text-decoration:none;margin-bottom:1rem}
.back-link:hover{text-decoration:underline}
table{width:100%;border-collapse:collapse;background:#fff;box-shadow:0 1px 3px rgba(0,0,0,0.1)}
th,td{padding:.75rem;text-align:left;border-bottom:1px solid #ddd;font-size:.9rem}
th{background:#f9f9f9;font-weight:600}
tr:hover{background:#fafafa}
td{word-break:break-word}
.empty-msg{padding:2rem;text-align:center;color:#666;font-style:italic}
</style>
</head>
<body>
<a href="{{.PanelPath}}/" class="back-link">← Dashboard</a>
<h1>Audit Log</h1>
<table>
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
<tr><td colspan="6" class="empty-msg">No entries.</td></tr>
{{end}}
</tbody>
</table>
</body>
</html>`))

func auditPage(w io.Writer, panelPath string, entries []auditEntry) {
	auditTmpl.Execute(w, map[string]any{ //nolint:errcheck
		"PanelPath": panelPath,
		"Entries":   entries,
	})
}
