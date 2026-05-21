package panel

import (
	"html/template"
	"reflect"
)

const sharedTopbar = `<header class="topbar">
  <a class="brand" href="{{.PanelPath}}dashboard">
    <span class="brand-mark">M</span>
    <span class="brand-copy">
      <span class="brand-name">MTProto Orchestrator</span>
      <span class="brand-sub">Admin Panel</span>
    </span>
  </a>
  <nav class="nav" aria-label="Primary">
    <a class="nav-item"{{if navIsCurrent . "dashboard"}} data-active="true"{{end}} href="{{.PanelPath}}dashboard">Dashboard</a>
    <a class="nav-item"{{if navIsCurrent . "users"}} data-active="true"{{end}} href="{{.PanelPath}}users">Users</a>
    <a class="nav-item"{{if navIsCurrent . "bridge"}} data-active="true"{{end}} href="{{.PanelPath}}bridge">Bridge</a>
    <a class="nav-item"{{if navIsCurrent . "logs"}} data-active="true"{{end}} href="{{.PanelPath}}logs">Logs</a>
    <a class="nav-item"{{if navIsCurrent . "stubs"}} data-active="true"{{end}} href="{{.PanelPath}}settings/stubs">Stubs</a>
    <a class="nav-item"{{if navIsCurrent . "certificates"}} data-active="true"{{end}} href="{{.PanelPath}}settings/certificates">Certificates</a>
    <a class="nav-item"{{if navIsCurrent . "settings"}} data-active="true"{{end}} href="{{.PanelPath}}settings/proxy">Settings</a>
  </nav>
  <div class="nav-spacer"></div>
  <form method="post" action="{{.PanelPath}}logout" class="inline logout-form">
    <input type="hidden" name="{{csrfField .}}" value="{{csrfToken .}}" class="js-csrf">
    <button type="submit" class="topbar-icon-btn" title="Sign out"><span aria-hidden="true">&rarr;</span><span class="sr-only">Sign out</span></button>
  </form>
  <div class="avatar" aria-hidden="true">AD</div>
</header>`

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
<div class="app">
` + sharedTopbar + `
<main class="page">
{{block "content" .}}{{end}}
</main>
</div>
</body>
</html>
{{end}}`

var baseLayoutFuncs = template.FuncMap{
	"csrfField":    layoutCSRFField,
	"csrfToken":    layoutCSRFToken,
	"navIsCurrent": layoutNavIsCurrent,
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

func layoutNavIsCurrent(data any, section string) bool {
	return stringField(data, "CurrentNav") == section
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
