package panel

import (
	"html/template"
	"reflect"
)

const baseLayout = `{{define "page_title"}}MTProto Orchestrator{{end}}
{{define "base"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{template "page_title" .}}</title>
<link rel="stylesheet" href="{{.PanelPath}}assets/panel.css">
<meta name="htmx-config" content='{"includeIndicatorStyles":false}'>
<script defer src="{{.PanelPath}}assets/vendor/htmx-2.0.10.min.js"></script>
<script defer src="{{.PanelPath}}assets/vendor/htmx-ext-sse-2.2.4.js"></script>
<script defer src="{{.PanelPath}}assets/panel.js"></script>
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
    <form method="post" action="{{.PanelPath}}logout" class="inline"><input type="hidden" name="{{csrfField .}}" value="{{csrfToken .}}" class="js-csrf"><button type="submit" class="logout">Logout</button></form>
</nav>
{{block "content" .}}{{end}}
</main>
</body>
</html>
{{end}}`

var baseLayoutFuncs = template.FuncMap{
	"csrfField": layoutCSRFField,
	"csrfToken": layoutCSRFToken,
}

func layoutTemplate(name, content string, funcMap template.FuncMap) *template.Template {
	merged := template.FuncMap{}
	for name, fn := range baseLayoutFuncs {
		merged[name] = fn
	}
	for name, fn := range funcMap {
		merged[name] = fn
	}
	t := template.New(name).Funcs(merged)
	return template.Must(template.Must(t.Parse(baseLayout)).Parse(content))
}

func layoutCSRFField(_ any) string {
	return CSRFField()
}

func layoutCSRFToken(data any) string {
	return stringField(data, "CSRFToken")
}

func stringField(data any, name string) string {
	if data == nil {
		return ""
	}
	if m, ok := data.(map[string]any); ok {
		if v, ok := m[name].(string); ok {
			return v
		}
		return ""
	}

	value := reflect.ValueOf(data)
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return ""
	}
	field := value.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}
