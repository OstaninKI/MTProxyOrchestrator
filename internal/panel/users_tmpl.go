package panel

import (
	"html/template"
	"io"
)

var userListTmpl = template.Must(template.New("users").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Users</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1{margin-bottom:1rem}
table{border-collapse:collapse;width:100%;margin-bottom:2rem}th,td{text-align:left;padding:.5rem;border-bottom:1px solid #e5e7eb}
.badge-on{color:#16a34a}.badge-off{color:#dc2626}
input[type=text]{padding:.4rem .6rem;border:1px solid #ccc;border-radius:4px;margin-right:.5rem}
button{padding:.4rem .8rem;background:#2563eb;color:#fff;border:none;border-radius:4px;cursor:pointer}
button:hover{background:#1d4ed8}.error{color:#dc2626;margin-bottom:1rem}
a{color:#2563eb}</style>
</head>
<body>
<h1>Users</h1>
<a href="../">← Dashboard</a>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" action="users/create">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<input type="text" name="label" placeholder="label (a-z0-9_)" required>
<button type="submit">Add user</button>
</form>
<table>
<thead><tr><th>Label</th><th>Status</th><th>Created</th><th>Actions</th></tr></thead>
<tbody>
{{range .Users}}
<tr>
  <td>{{.Label}}</td>
  <td>{{if .Enabled}}<span class="badge-on">enabled</span>{{else}}<span class="badge-off">disabled</span>{{end}}</td>
  <td>{{.CreatedAt.Format "2006-01-02"}}</td>
  <td>
    <form method="post" action="users/{{.ID}}/toggle" style="display:inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">{{if .Enabled}}Disable{{else}}Enable{{end}}</button>
    </form>
    <form method="post" action="users/{{.ID}}/rotate" style="display:inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">Rotate</button>
    </form>
    <form method="post" action="users/{{.ID}}/delete" style="display:inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit" onclick="return confirm('Delete {{.Label}}?')">Delete</button>
    </form>
  </td>
</tr>
{{end}}
</tbody>
</table>
</body>
</html>
`))

var userCreatedTmpl = template.Must(template.New("user_created").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>User created</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}.card{background:#f0fdf4;border:1px solid #bbf7d0;border-radius:8px;padding:1.5rem;max-width:600px}
code{background:#e5e7eb;padding:.2rem .4rem;border-radius:3px;font-family:monospace;word-break:break-all}
a{color:#2563eb}.warn{color:#d97706;font-size:.875rem;margin-top:.5rem}</style>
</head>
<body>
<h1>User created</h1>
<div class="card">
<p><strong>Label:</strong> {{.Label}}</p>
<p><strong>Telegram link:</strong><br><code>{{.TelegramURL}}</code></p>
<p class="warn">⚠ Save this link — the secret will not be shown again.</p>
</div>
<p><a href="../users">← Back to users</a></p>
</body>
</html>
`))

type userListData struct {
	Users     []UserRow
	CSRFField string
	CSRFToken string
	Error     string
}

type userCreatedData struct {
	Label       string
	TelegramURL string
}

func userListPage(w io.Writer, users []UserRow, csrfToken, errMsg string) {
	userListTmpl.Execute(w, userListData{ //nolint:errcheck
		Users:     users,
		CSRFField: CSRFField(),
		CSRFToken: csrfToken,
		Error:     errMsg,
	})
}

func userCreatedPage(w io.Writer, label, secretHex string) {
	link := ProxyLink{
		Server:    "<your-server>",
		Port:      443,
		SecretHex: secretHex,
		MaskHost:  "www.microsoft.com",
	}
	userCreatedTmpl.Execute(w, userCreatedData{ //nolint:errcheck
		Label:       label,
		TelegramURL: link.TelegramURL(),
	})
}
