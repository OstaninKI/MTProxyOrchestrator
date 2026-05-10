package panel

import "html/template"

const baseLayout = `{{define "page_title"}}MTProto Orchestrator{{end}}
{{define "base"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{template "page_title" .}}</title>
<style>
:root{--bg:#09090b;--card:#101014;--card2:#15151b;--border:#27272f;--text:#f4f4f5;--muted:#a1a1aa;--accent:#38bdf8;--good:#22c55e;--bad:#f43f5e}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;background:radial-gradient(circle at 18% -8%,rgba(56,189,248,.2),transparent 34%),linear-gradient(135deg,#09090b,#111116 48%,#0c0f14);color:var(--text);font-family:Aptos,"Segoe UI",sans-serif}
a{color:var(--accent);text-decoration:none}a:hover{text-decoration:underline}
.shell{position:relative;max-width:1240px;margin:0 auto;padding:28px}
.nav{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:22px}
.nav a,.logout{display:inline-flex;align-items:center;height:34px;padding:0 12px;border-radius:7px;border:1px solid var(--border);background:rgba(255,255,255,.04);color:var(--text);text-decoration:none;font-size:.86rem;cursor:pointer}
.nav a:hover,.logout:hover{border-color:rgba(56,189,248,.55);background:rgba(56,189,248,.1);text-decoration:none}
.logout{font:inherit}
h1{font-size:2.15rem;line-height:1;margin:0 0 14px}h2{font-size:1rem;margin:0 0 14px}
table{width:100%;border-collapse:collapse}
th,td{text-align:left;padding:12px 14px;border-bottom:1px solid var(--border);font-size:.9rem}
th{color:var(--muted);font-size:.74rem;text-transform:uppercase;letter-spacing:.08em;background:rgba(255,255,255,.03)}
tr:last-child td{border-bottom:0}
.card{background:linear-gradient(180deg,var(--card),var(--card2));border:1px solid var(--border);border-radius:8px;padding:18px}
.table-wrap{overflow:auto;border:1px solid var(--border);border-radius:8px}
.badge{display:inline-flex;align-items:center;height:24px;border-radius:999px;padding:0 9px;border:1px solid var(--border);font-size:.78rem}
.ok{color:var(--good);background:rgba(34,197,94,.1);border-color:rgba(34,197,94,.22)}
.down,.bad{color:var(--bad);background:rgba(244,63,94,.1);border-color:rgba(244,63,94,.24)}
.success{color:var(--good);margin-bottom:1rem}
.error{color:var(--bad);margin-bottom:1rem}
.warn{color:#d97706}
label{display:block;margin-top:1rem;font-weight:bold;color:var(--muted);font-size:.86rem}
input[type=text],input[type=password],input[type=number],input[type=file],select,textarea{width:100%;padding:.5rem .75rem;margin-top:.25rem;border:1px solid var(--border);border-radius:6px;background:var(--card);color:var(--text);font-size:.9rem;box-sizing:border-box}
input:focus,select:focus,textarea:focus{outline:none;border-color:var(--accent)}
button{padding:.5rem 1rem;background:rgba(56,189,248,.15);color:var(--accent);border:1px solid rgba(56,189,248,.3);border-radius:6px;cursor:pointer;font-size:.86rem}
button:hover{background:rgba(56,189,248,.25)}
.danger{background:rgba(244,63,94,.15);color:var(--bad);border-color:rgba(244,63,94,.3)}
.danger:hover{background:rgba(244,63,94,.25)}
.muted{color:var(--muted)}
code{background:var(--card2);padding:.2rem .4rem;border-radius:3px;font-family:monospace;word-break:break-all}
.warn-box{background:rgba(217,119,6,.1);border:1px solid rgba(217,119,6,.25);border-radius:6px;padding:1rem;margin-bottom:1.5rem;color:#d97706}
.info-box{background:rgba(56,189,248,.08);border:1px solid rgba(56,189,248,.2);border-radius:6px;padding:1rem;margin-bottom:1.5rem;color:var(--text)}
form.inline{display:inline}
</style>
{{block "head" .}}{{end}}
</head>
<body>
<main class="shell">
<nav class="nav" aria-label="Primary">
    <a href="{{.PanelPath}}users">Users</a>
    <a href="{{.PanelPath}}bridge">Bridge</a>
    <a href="{{.PanelPath}}logs">Logs</a>
    <a href="{{.PanelPath}}settings/stubs">Stubs</a>
    <a href="{{.PanelPath}}settings/certificates">Certificates</a>
    <a href="{{.PanelPath}}settings/proxy">Proxy</a>
    <a href="{{.PanelPath}}settings/admin-password">Password</a>
    <a href="{{.PanelPath}}settings/system">System</a>
    <form method="post" action="{{.PanelPath}}logout" style="display:inline"><input type="hidden" name="_csrf" class="js-csrf"><button type="submit" class="logout">Logout</button></form>
</nav>
{{block "content" .}}{{end}}
</main>
<script>(function(){var m=document.cookie.match(/(?:^|;)\s*csrf_token=([^;]+)/);if(m)document.querySelectorAll('.js-csrf').forEach(function(el){el.value=decodeURIComponent(m[1]);})})();</script>
</body>
</html>
{{end}}`

func layoutTemplate(name, content string, funcMap template.FuncMap) *template.Template {
	t := template.New(name)
	if funcMap != nil {
		t = t.Funcs(funcMap)
	}
	return template.Must(template.Must(t.Parse(baseLayout)).Parse(content))
}
