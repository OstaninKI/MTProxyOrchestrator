package panel

import (
	"html/template"
	"io"
)

var loginTmpl = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Login</title>
<style>body{font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#f5f5f5}
.card{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,.12);width:320px}
h1{margin:0 0 1.5rem;font-size:1.4rem;color:#333}
label{display:block;margin-bottom:.25rem;font-size:.875rem;color:#555}
input{width:100%;box-sizing:border-box;padding:.5rem;border:1px solid #ccc;border-radius:4px;margin-bottom:1rem;font-size:1rem}
button{width:100%;padding:.6rem;background:#2563eb;color:#fff;border:none;border-radius:4px;font-size:1rem;cursor:pointer}
button:hover{background:#1d4ed8}.error{color:#dc2626;margin-bottom:1rem;font-size:.875rem}</style>
</head>
<body>
<div class="card">
<h1>Admin Login</h1>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Login</label><input type="text" name="login" autocomplete="username" required>
<label>Password</label><input type="password" name="password" autocomplete="current-password" required>
<button type="submit">Sign in</button>
</form>
</div>
</body>
</html>
`))

var dashboardTmpl = template.Must(template.New("dashboard").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Dashboard</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1{margin-bottom:1rem}a{color:#2563eb}</style>
</head>
<body>
<h1>Dashboard</h1>
<p>Welcome to tgproxy admin panel.</p>
<a href="logout">Logout</a>
</body>
</html>
`))

func loginPage(w io.Writer, csrfToken, errMsg string) {
	loginTmpl.Execute(w, map[string]string{ //nolint:errcheck
		"CSRFField": CSRFField(),
		"CSRFToken": csrfToken,
		"Error":     errMsg,
	})
}

func dashboardPage(w io.Writer) {
	dashboardTmpl.Execute(w, nil) //nolint:errcheck
}
