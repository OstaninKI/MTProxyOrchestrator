package panel

import (
	"fmt"
	"html"
	"html/template"
	"reflect"
	"strings"
)

const sharedTopbar = `<header class="topbar">
  <a class="brand" href="{{.PanelPath}}dashboard">
    <span class="brand-mark">M</span>
    <span class="brand-name">MTProto Orchestrator</span>
  </a>
  <nav class="nav" aria-label="Primary">
    <a class="nav-item"{{if navIsCurrent . "dashboard"}} data-active="true"{{end}} href="{{.PanelPath}}dashboard">{{icon "Dashboard" 14}} Dashboard</a>
    <a class="nav-item"{{if navIsCurrent . "users"}} data-active="true"{{end}} href="{{.PanelPath}}users">{{icon "Users" 14}} Users</a>
    <a class="nav-item"{{if navIsCurrent . "bridge"}} data-active="true"{{end}} href="{{.PanelPath}}bridge">{{icon "Bridge" 14}} Bridge</a>
    <a class="nav-item"{{if navIsCurrent . "logs"}} data-active="true"{{end}} href="{{.PanelPath}}logs">{{icon "Logs" 14}} Logs</a>
    <a class="nav-item"{{if navIsCurrent . "stubs"}} data-active="true"{{end}} href="{{.PanelPath}}settings/stubs">{{icon "Stubs" 14}} Stubs</a>
    <a class="nav-item"{{if navIsCurrent . "certificates"}} data-active="true"{{end}} href="{{.PanelPath}}settings/certificates">{{icon "Cert" 14}} Certificates</a>
    <a class="nav-item"{{if navIsCurrent . "settings"}} data-active="true"{{end}} href="{{.PanelPath}}settings/proxy">{{icon "Settings" 14}} Settings</a>
  </nav>
  <div class="nav-spacer"></div>
  <button class="cmd-k" type="button" disabled>
    {{icon "Search" 13}}
    <span class="ph">Search</span>
    <kbd>⌘K</kbd>
  </button>
  <button class="topbar-icon-btn" type="button" title="Notifications" disabled>
    {{icon "Bell" 15}}
    <span class="dot"></span>
  </button>
  <form method="post" action="{{.PanelPath}}logout" class="inline logout-form">
    <input type="hidden" name="{{csrfField .}}" value="{{csrfToken .}}" class="js-csrf">
    <button type="submit" class="topbar-icon-btn" title="Sign out">{{icon "Logout" 15}}<span class="sr-only">Sign out</span></button>
  </form>
  <div class="avatar" aria-hidden="true">PE</div>
</header>`

const baseLayout = `{{define "page_title"}}MTProto Orchestrator{{end}}
{{define "base"}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{template "page_title" .}}</title>
<link rel="preload" href="{{.PanelPath}}assets/fonts/geist/Geist-Regular.woff2" as="font" type="font/woff2" crossorigin>
<link rel="preload" href="{{.PanelPath}}assets/fonts/geist/Geist-Medium.woff2" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="{{.PanelPath}}assets/panel.css{{assetv "panel.css"}}">
<meta name="htmx-config" content='{"includeIndicatorStyles":false}'>
<script defer src="{{.PanelPath}}assets/vendor/htmx-2.0.10.min.js"></script>
<script defer src="{{.PanelPath}}assets/vendor/htmx-ext-sse-2.2.4.js"></script>
<script defer src="{{.PanelPath}}assets/panel.js{{assetv "panel.js"}}"></script>
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
	"icon":         layoutIcon,
	"assetv":       assetVersion,
	"settingsTabs": settingsTabsNav,
}

// settingsTabsNav renders the Settings sub-navigation as a segmented control.
// The links carry htmx attributes so that switching tabs swaps only the
// #settings-tabs panel out of the full-page response (hx-select), giving a
// true in-page tab experience without a full reload, while the plain href
// keeps the tabs working without JavaScript and preserves deep links.
func settingsTabsNav(panelPath, active string) template.HTML {
	tabs := []struct {
		key   string
		path  string
		label string
	}{
		{"proxy", "settings/proxy", "Endpoint & Proxy"},
		{"admin-password", "settings/admin-password", "Admin password"},
		{"system", "settings/system", "System"},
		{"totp", "settings/totp", "Two-factor"},
	}
	var b strings.Builder
	b.WriteString(`<nav class="seg" aria-label="Settings tabs">`)
	for _, t := range tabs {
		cls := "seg-item"
		if t.key == active {
			cls += " active"
		}
		href := html.EscapeString(panelPath + t.path)
		fmt.Fprintf(&b, `<a class="%s" href="%s" hx-get="%s" hx-target="#settings-tabs" hx-select="#settings-tabs" hx-swap="outerHTML" hx-push-url="true">%s</a>`,
			cls, href, href, html.EscapeString(t.label))
	}
	b.WriteString(`</nav>`)
	return template.HTML(b.String()) //nolint:gosec // panelPath is trusted config; href and label are escaped
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

func layoutIcon(name string, size int) template.HTML {
	if size <= 0 {
		size = 16
	}
	paths := map[string]string{
		"Dashboard":    `<rect x="3" y="3" width="7" height="9"/><rect x="14" y="3" width="7" height="5"/><rect x="14" y="12" width="7" height="9"/><rect x="3" y="16" width="7" height="5"/>`,
		"Users":        `<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>`,
		"Bridge":       `<path d="M3 12h18"/><path d="M3 8v8"/><path d="M21 8v8"/><path d="M7 12V8"/><path d="M12 12V6"/><path d="M17 12V8"/>`,
		"Logs":         `<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><path d="M8 13h8"/><path d="M8 17h5"/>`,
		"Stubs":        `<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18"/><path d="M9 21V9"/>`,
		"Cert":         `<circle cx="12" cy="9" r="5"/><path d="M9 13.5V21l3-2 3 2v-7.5"/>`,
		"Settings":     `<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>`,
		"Search":       `<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>`,
		"Bell":         `<path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9"/><path d="M10.3 21a1.94 1.94 0 0 0 3.4 0"/>`,
		"Logout":       `<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5"/><path d="M21 12H9"/>`,
		"Activity":     `<path d="M22 12h-4l-3 9L9 3l-3 9H2"/>`,
		"Plus":         `<path d="M12 5v14M5 12h14"/>`,
		"Check":        `<path d="M20 6 9 17l-5-5"/>`,
		"Trash":        `<path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/>`,
		"Edit":         `<path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 1 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>`,
		"Power":        `<path d="M18.36 6.64a9 9 0 1 1-12.72 0"/><path d="M12 2v10"/>`,
		"Pause":        `<rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/>`,
		"Play":         `<polygon points="5,3 19,12 5,21"/>`,
		"Download":     `<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m7 10 5 5 5-5"/><path d="M12 15V3"/>`,
		"Upload":       `<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m17 8-5-5-5 5"/><path d="M12 3v12"/>`,
		"Info":         `<circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/>`,
		"X":            `<path d="M18 6 6 18M6 6l12 12"/>`,
		"Lock":         `<rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>`,
		"Globe":        `<circle cx="12" cy="12" r="10"/><path d="M2 12h20"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>`,
		"Shield":       `<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>`,
		"Copy":         `<rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>`,
		"Key":          `<circle cx="7.5" cy="15.5" r="3.5"/><path d="m21 2-9.6 9.6"/><path d="m15.5 7.5 3 3L22 7l-3-3"/>`,
		"Cloud":        `<path d="M17.5 19a4.5 4.5 0 0 0 0-9H17a7 7 0 1 0-12.95 4.5"/><path d="M12 13v8"/><path d="m8 17 4-4 4 4"/>`,
		"Refresh":      `<path d="M3 12a9 9 0 0 1 15-6.7L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-15 6.7L3 16"/><path d="M3 21v-5h5"/>`,
		"Heart":        `<path d="m20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"/>`,
		"ArrowUpRight": `<path d="m7 17 10-10M7 7h10v10"/>`,
		"Right":        `<path d="m9 18 6-6-6-6"/>`,
		"More":         `<circle cx="5" cy="12" r="1.5"/><circle cx="12" cy="12" r="1.5"/><circle cx="19" cy="12" r="1.5"/>`,
		"Up":           `<path d="m18 15-6-6-6 6"/>`,
		"Down":         `<path d="m6 9 6 6 6-6"/>`,
		"Server":       `<rect x="2" y="3" width="20" height="8" rx="2"/><rect x="2" y="13" width="20" height="8" rx="2"/><path d="M6 7h.01M6 17h.01"/>`,
	}
	body, ok := paths[name]
	if !ok {
		return ""
	}
	return template.HTML(fmt.Sprintf(`<svg width="%d" height="%d" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">%s</svg>`, size, size, body))
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
