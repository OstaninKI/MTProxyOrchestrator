package panel

import (
	"html/template"
	"io"
)

var dashboardFragmentFuncs = template.FuncMap{
	"formatBytes": func(n int64) string {
		if n < 0 {
			return "0 B"
		}
		return formatBytes(uint64(n))
	},
}

const dashboardFragments = `
{{define "health"}}
<section
  id="dashboard-health"
  class="card span2 ops-card ops-health"
  hx-get="{{.PanelPath}}dashboard/fragments/health?period={{.Period}}"
  hx-trigger="sse:dashboard-health"
  hx-swap="outerHTML">
{{if .IsBridge}}
<h2>Bridge Mode — Chain Health</h2>
<div class="table-wrap"><table>
<thead><tr><th>Step</th><th>Status</th><th>Latency</th><th>Message</th></tr></thead>
<tbody>
{{range .BridgeSteps}}
<tr>
  <td>{{.Name}}</td>
  <td>{{if .OK}}<span class="badge ok">ok</span>{{else}}<span class="badge down">down</span>{{end}}</td>
  <td>{{if .Latency}}{{.Latency}}{{else}}&mdash;{{end}}</td>
  <td>{{.Message}}</td>
</tr>
{{end}}
</tbody>
</table></div>
{{else}}
<h2>Services</h2>
<div class="table-wrap"><table>
<thead><tr><th>Service</th><th>Status</th><th>Message</th></tr></thead>
<tbody>
{{range .Services}}
<tr>
  <td>{{.Name}}</td>
  <td>{{if .Active}}<span class="badge ok">running</span>{{else}}<span class="badge down">down</span>{{end}}</td>
  <td>{{.Message}}</td>
</tr>
{{end}}
</tbody>
</table></div>
{{end}}
</section>
{{end}}

{{define "connections"}}
<section
  id="dashboard-connections"
  class="card ops-card ops-connections"
  hx-get="{{.PanelPath}}dashboard/fragments/connections?period={{.Period}}"
  hx-trigger="sse:dashboard-connections"
  hx-swap="outerHTML">
<h2>Active Connections</h2>
{{if .LiveConnections}}
<div class="table-wrap"><table>
<thead><tr><th>User</th><th>Active Connections</th></tr></thead>
<tbody>
{{range .LiveConnections}}
<tr>
  <td>{{.UserLabel}}</td>
  <td>{{.Connections}}</td>
</tr>
{{end}}
</tbody>
</table></div>
{{else}}
<p class="empty">No active connections.</p>
{{end}}
</section>
{{end}}

{{define "traffic"}}
<section
  id="dashboard-traffic"
  class="card span2 ops-card ops-traffic"
  hx-get="{{.PanelPath}}dashboard/fragments/traffic?period={{.Period}}"
  hx-trigger="sse:dashboard-traffic"
  hx-swap="outerHTML">
<h2>Top Users</h2>
<div class="periods">
  <a href="?period=1h"{{if eq .Period "1h"}} class="active"{{end}}>1h</a>
  <a href="?period=24h"{{if eq .Period "24h"}} class="active"{{end}}>24h</a>
  <a href="?period=7d"{{if eq .Period "7d"}} class="active"{{end}}>7d</a>
  <a href="?period=30d"{{if eq .Period "30d"}} class="active"{{end}}>30d</a>
</div>
{{if .TopUsers}}
<div class="table-wrap"><table>
<thead><tr><th>User</th><th>Bytes In</th><th>Bytes Out</th><th>Connections</th></tr></thead>
<tbody>
{{range .TopUsers}}
<tr>
  <td>{{.UserLabel}}</td>
  <td>{{formatBytes .BytesIn}}</td>
  <td>{{formatBytes .BytesOut}}</td>
  <td>{{.Connections}}</td>
</tr>
{{end}}
</tbody>
</table></div>
{{else}}
<p class="empty">No traffic data for this period.</p>
{{end}}
</section>
{{end}}

{{define "components"}}
<section
  id="dashboard-components"
  class="card ops-card ops-components"
  hx-get="{{.PanelPath}}dashboard/fragments/components?period={{.Period}}"
  hx-trigger="sse:dashboard-components"
  hx-swap="outerHTML">
<h2>Components</h2>
<div class="table-wrap"><table>
<thead><tr><th>Component</th><th>Version</th></tr></thead>
<tbody>
{{range .Components}}
<tr><td>{{.Name}}</td><td>{{.Version}}</td></tr>
{{end}}
</tbody>
</table></div>
</section>
{{end}}
`

var dashboardFragmentsTmpl = template.Must(template.New("dashboard_fragments").Funcs(dashboardFragmentFuncs).Parse(dashboardFragments))

func dashboardHealthFragment(w io.Writer, data DashboardData) {
	dashboardFragmentsTmpl.ExecuteTemplate(w, "health", data) //nolint:errcheck
}

func dashboardConnectionsFragment(w io.Writer, data DashboardData) {
	dashboardFragmentsTmpl.ExecuteTemplate(w, "connections", data) //nolint:errcheck
}

func dashboardTrafficFragment(w io.Writer, data DashboardData) {
	dashboardFragmentsTmpl.ExecuteTemplate(w, "traffic", data) //nolint:errcheck
}

func dashboardComponentsFragment(w io.Writer, data DashboardData) {
	dashboardFragmentsTmpl.ExecuteTemplate(w, "components", data) //nolint:errcheck
}
