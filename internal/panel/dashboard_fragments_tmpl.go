package panel

import (
	"fmt"
	"html/template"
	"io"
	"math"
	"strings"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
)

var dashboardFragmentFuncs = template.FuncMap{
	"csrfField":    layoutCSRFField,
	"csrfToken":    layoutCSRFToken,
	"navIsCurrent": layoutNavIsCurrent,
	"formatBytes": func(n int64) string {
		if n < 0 {
			return "0 B"
		}
		return formatBytes(uint64(n))
	},
	"trafficTotal": func(in, out int64) int64 {
		if in < 0 {
			in = 0
		}
		if out < 0 {
			out = 0
		}
		return in + out
	},
	"countEnabledUsers": func(users []UserRow) int {
		count := 0
		for _, user := range users {
			if user.Enabled {
				count++
			}
		}
		return count
	},
	"countEnabledNodes": func(nodes []bridge.Node) int {
		count := 0
		for _, node := range nodes {
			if node.Enabled {
				count++
			}
		}
		return count
	},
	"previewBridgeNodes": func(nodes []bridge.Node, limit int) []bridge.Node {
		if limit <= 0 || len(nodes) <= limit {
			return nodes
		}
		return nodes[:limit]
	},
	"sumTrafficSeries": func(series []metrics.TrafficBucket) int64 {
		var total int64
		for _, bucket := range series {
			total += bucket.BytesIn + bucket.BytesOut
		}
		return total
	},
	"sumTrafficInSeries": func(series []metrics.TrafficBucket) int64 {
		var total int64
		for _, bucket := range series {
			total += bucket.BytesIn
		}
		return total
	},
	"sumTrafficOutSeries": func(series []metrics.TrafficBucket) int64 {
		var total int64
		for _, bucket := range series {
			total += bucket.BytesOut
		}
		return total
	},
	"sumLiveConnections": func(samples []metrics.Sample) int64 {
		var total int64
		for _, sample := range samples {
			if sample.Connections > 0 {
				total += sample.Connections
			}
		}
		return total
	},
	"maxLiveConnections": func(samples []metrics.Sample) int64 {
		var max int64
		for _, sample := range samples {
			if sample.Connections > max {
				max = sample.Connections
			}
		}
		return max
	},
	"countActiveServices": func(services []health.ServiceState) int {
		count := 0
		for _, service := range services {
			if service.Active {
				count++
			}
		}
		return count
	},
	"countOKBridgeSteps": func(steps []health.BridgeStepStatus) int {
		count := 0
		for _, step := range steps {
			if step.OK {
				count++
			}
		}
		return count
	},
	"formatPercent": func(v float64) string {
		if v < 0 {
			return "unknown"
		}
		return fmt.Sprintf("%.0f%%", v)
	},
	"barWidth": func(v float64) string {
		if v < 0 {
			return "0%"
		}
		if v > 100 {
			v = 100
		}
		return fmt.Sprintf("%.0f%%", v)
	},
	"usageTone": func(v float64) string {
		switch {
		case v < 0:
			return "neutral"
		case v >= 85:
			return "danger"
		case v >= 65:
			return "warn"
		default:
			return "success"
		}
	},
	"trafficLinePath": func(series []metrics.TrafficBucket) string {
		return trafficLinePath(series)
	},
	"trafficAreaPath": func(series []metrics.TrafficBucket) string {
		return trafficAreaPath(series)
	},
	"trafficLinePathIn": func(series []metrics.TrafficBucket) string {
		return trafficLinePathBy(series, func(bucket metrics.TrafficBucket) int64 { return bucket.BytesIn })
	},
	"trafficAreaPathIn": func(series []metrics.TrafficBucket) string {
		return trafficAreaPathBy(series, func(bucket metrics.TrafficBucket) int64 { return bucket.BytesIn })
	},
	"trafficLinePathOut": func(series []metrics.TrafficBucket) string {
		return trafficLinePathBy(series, func(bucket metrics.TrafficBucket) int64 { return bucket.BytesOut })
	},
	"trafficAreaPathOut": func(series []metrics.TrafficBucket) string {
		return trafficAreaPathBy(series, func(bucket metrics.TrafficBucket) int64 { return bucket.BytesOut })
	},
	"componentStateLabel": func(name, version string, isBridge bool) string {
		if version == "" || version == "unknown" {
			if name == "sing-box" && !isBridge {
				return "optional"
			}
			return "missing"
		}
		if name == "sing-box" && !isBridge {
			return "standby"
		}
		return "installed"
	},
	"componentStateClass": func(name, version string, isBridge bool) string {
		if version == "" || version == "unknown" {
			if name == "sing-box" && !isBridge {
				return "warn"
			}
			return "down"
		}
		return "ok"
	},
	"componentNote": func(name string, isBridge bool) string {
		switch name {
		case "tgproxy-cli":
			return "Lifecycle, backup, restore, and update operations"
		case "tgproxy-panel":
			return "Admin UI, metrics sampling, and authenticated dashboard"
		case "teleproxy":
			return "MTProto ingress and Telegram transport runtime"
		case "sing-box":
			if isBridge {
				return "Required for Bridge routing and outbound chains"
			}
			return "Optional in Single mode"
		default:
			return "Installed runtime component"
		}
	},
	"latestTrafficTotal": func(series []metrics.TrafficBucket) int64 {
		if len(series) == 0 {
			return 0
		}
		last := series[len(series)-1]
		return last.BytesIn + last.BytesOut
	},
	"bridgeNodeStateLabel": func(node bridge.Node) string {
		if !node.Enabled {
			return "disabled"
		}
		if node.LastLatency > 0 {
			return "ready"
		}
		return "untested"
	},
	"bridgeNodeStateClass": func(node bridge.Node) string {
		if !node.Enabled {
			return "down"
		}
		if node.LastLatency > 0 {
			return "ok"
		}
		return "warn"
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
<div class="ops-card-head">
{{if .IsBridge}}
<div class="ops-card-title-row">
  <h2>Bridge Mode — Chain Health</h2>
  {{if .Healthy}}<span class="badge ok">healthy</span>{{else}}<span class="badge down">unhealthy</span>{{end}}
</div>
<p class="ops-card-sub">{{.HealthLabel}}. {{countEnabledNodes .BridgeNodes}} of {{len .BridgeNodes}} nodes enabled.</p>
{{else}}
<div class="ops-card-title-row">
  <h2>Services</h2>
  {{if .Healthy}}<span class="badge ok">healthy</span>{{else}}<span class="badge down">unhealthy</span>{{end}}
</div>
<p class="ops-card-sub">{{.HealthLabel}}. {{countEnabledUsers .Users}} of {{len .Users}} users enabled.</p>
{{end}}
</div>
<div class="ops-health-grid">
  <div class="ops-system-panel">
    <div class="ops-panel-head">
      <h3>System</h3>
      <span class="badge{{if .Healthy}} ok{{else}} down{{end}}">{{if .Healthy}}stable{{else}}attention{{end}}</span>
    </div>
    <div class="ops-system-metrics">
      <div class="ops-system-meter">
        <div class="ops-system-meter-copy"><span class="k">Memory</span><span class="v mono">{{formatPercent .System.MemoryPercent}}</span></div>
        <div class="ops-meter" data-tone="{{usageTone .System.MemoryPercent}}"><span style="width: {{barWidth .System.MemoryPercent}}"></span></div>
      </div>
      <div class="ops-system-meter">
        <div class="ops-system-meter-copy"><span class="k">Disk</span><span class="v mono">{{formatPercent .System.DiskPercent}}</span></div>
        <div class="ops-meter" data-tone="{{usageTone .System.DiskPercent}}"><span style="width: {{barWidth .System.DiskPercent}}"></span></div>
      </div>
    </div>
    <div class="ops-system-meta">
      <div class="ops-system-meta-row"><span class="k">Load avg</span><span class="v mono">{{.System.LoadAvg}}</span></div>
      <div class="ops-system-meta-row"><span class="k">Uptime</span><span class="v mono">{{.System.Uptime}}</span></div>
      <div class="ops-system-meta-row"><span class="k">Kernel</span><span class="v mono">{{.System.Kernel}}</span></div>
      <div class="ops-system-meta-row"><span class="k">Mode</span><span class="v mono">{{if .IsBridge}}Bridge{{else}}Single{{end}}</span></div>
    </div>
  </div>
  <div class="ops-service-panel">
    {{if .IsBridge}}
    <div class="ops-panel-head">
      <h3>Chain steps</h3>
      <span class="badge{{if .Healthy}} ok{{else}} down{{end}}">{{countOKBridgeSteps .BridgeSteps}} / {{len .BridgeSteps}} ok</span>
    </div>
    <div class="ops-inline-metrics">
      <div class="ops-inline-metric"><span class="k">Users</span><span class="v mono">{{countEnabledUsers .Users}} / {{len .Users}}</span></div>
      <div class="ops-inline-metric"><span class="k">Nodes</span><span class="v mono">{{countEnabledNodes .BridgeNodes}} / {{len .BridgeNodes}}</span></div>
      <div class="ops-inline-metric"><span class="k">Load</span><span class="v mono">{{.System.LoadAvg}}</span></div>
    </div>
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
    <div class="ops-panel-head">
      <h3>Service runtime</h3>
      <span class="badge{{if .Healthy}} ok{{else}} down{{end}}">{{countActiveServices .Services}} / {{len .Services}} running</span>
    </div>
    <div class="ops-inline-metrics">
      <div class="ops-inline-metric"><span class="k">Enabled users</span><span class="v mono">{{countEnabledUsers .Users}} / {{len .Users}}</span></div>
      <div class="ops-inline-metric"><span class="k">Active users</span><span class="v mono">{{len .LiveConnections}}</span></div>
      <div class="ops-inline-metric"><span class="k">Total live</span><span class="v mono">{{sumLiveConnections .LiveConnections}}</span></div>
    </div>
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
  </div>
</div>
</section>
{{end}}

{{define "connections"}}
<section
  id="dashboard-connections"
  class="card ops-card ops-connections"
  hx-get="{{.PanelPath}}dashboard/fragments/connections?period={{.Period}}"
  hx-trigger="sse:dashboard-connections"
  hx-swap="outerHTML">
<div class="ops-card-head">
  <div class="ops-card-title-row">
    <h2>Active Connections</h2>
    <span class="badge ok mono">{{sumLiveConnections .LiveConnections}} live</span>
  </div>
  <p class="ops-card-sub">{{len .LiveConnections}} users active now, {{len .Users}} configured in panel.</p>
</div>
<div class="connection-summary">
  <div class="connection-stat"><span class="k">Users live</span><strong class="mono">{{len .LiveConnections}}</strong></div>
  <div class="connection-stat"><span class="k">Connections</span><strong class="mono">{{sumLiveConnections .LiveConnections}}</strong></div>
  <div class="connection-stat"><span class="k">Configured</span><strong class="mono">{{len .Users}}</strong></div>
  <div class="connection-stat"><span class="k">Peak / user</span><strong class="mono">{{maxLiveConnections .LiveConnections}}</strong></div>
</div>
{{if .LiveConnections}}
<div class="table-wrap"><table>
<thead><tr><th>User</th><th>Live</th></tr></thead>
<tbody>
{{range .LiveConnections}}
<tr>
  <td><span class="connection-user">{{.UserLabel}}</span></td>
  <td><span class="badge ok mono">{{.Connections}}</span></td>
</tr>
{{end}}
</tbody>
</table></div>
<nav class="page-nav" aria-label="Connection summary links">
  <a href="{{.PanelPath}}users">Open Users</a>
</nav>
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
<div class="ops-card-title-row traffic-head">
<h2>Network Throughput</h2>
<div class="periods">
  <a href="?period=1h"{{if eq .Period "1h"}} class="active"{{end}}>1h</a>
  <a href="?period=24h"{{if eq .Period "24h"}} class="active"{{end}}>24h</a>
  <a href="?period=7d"{{if eq .Period "7d"}} class="active"{{end}}>7d</a>
  <a href="?period=30d"{{if eq .Period "30d"}} class="active"{{end}}>30d</a>
</div>
</div>
<div class="traffic-overview">
  <div>
    <p class="ops-card-sub">Aggregate ingress and egress across the selected window.</p>
    <div class="traffic-legend">
      <div class="traffic-legend-item traffic-legend-item-out"><span class="swatch"></span><span>Download</span><strong>{{formatBytes (sumTrafficOutSeries .TrafficSeries)}}</strong></div>
      <div class="traffic-legend-item traffic-legend-item-in"><span class="swatch"></span><span>Upload</span><strong>{{formatBytes (sumTrafficInSeries .TrafficSeries)}}</strong></div>
    </div>
    <div class="traffic-kpis">
      <div class="traffic-kpi">
        <span class="k">Transferred</span>
        <strong>{{formatBytes (sumTrafficSeries .TrafficSeries)}}</strong>
      </div>
      <div class="traffic-kpi">
        <span class="k">Users</span>
        <strong>{{len .TopUsers}}</strong>
      </div>
      <div class="traffic-kpi">
        <span class="k">Peak connections</span>
        <strong>{{maxLiveConnections .LiveConnections}}</strong>
      </div>
    </div>
  </div>
  {{if .TrafficSeries}}
  <div class="traffic-chart-shell" aria-hidden="true">
    <svg class="traffic-chart" viewBox="0 0 100 56" preserveAspectRatio="none">
      <path class="traffic-chart-area traffic-chart-area-out" d="{{trafficAreaPathOut .TrafficSeries}}"></path>
      <path class="traffic-chart-line traffic-chart-line-out" d="{{trafficLinePathOut .TrafficSeries}}"></path>
      <path class="traffic-chart-area traffic-chart-area-in" d="{{trafficAreaPathIn .TrafficSeries}}"></path>
      <path class="traffic-chart-line traffic-chart-line-in" d="{{trafficLinePathIn .TrafficSeries}}"></path>
    </svg>
  </div>
  {{end}}
</div>
{{if .TopUsers}}
<div class="traffic-table-head">
  <h3>Top users by traffic</h3>
  <p class="ops-card-sub">Real traffic totals over the selected period.</p>
</div>
<div class="table-wrap"><table>
<thead><tr><th>User</th><th>Downloaded</th><th>Uploaded</th><th>Total</th><th>Connections</th></tr></thead>
<tbody>
{{range .TopUsers}}
<tr>
  <td><span class="traffic-user">{{.UserLabel}}</span></td>
  <td>{{formatBytes .BytesOut}}</td>
  <td>{{formatBytes .BytesIn}}</td>
  <td>{{formatBytes (trafficTotal .BytesIn .BytesOut)}}</td>
  <td><span class="badge{{if gt .Connections 0}} ok{{else}} warn{{end}} mono">{{.Connections}}</span></td>
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
<p class="ops-card-sub">Installed components and runtime readiness.</p>
<div class="component-grid">
{{range .Components}}
  <article class="component-card">
    <div class="component-card-head">
      <div>
        <h3>{{.Name}}</h3>
        <p>{{componentNote .Name $.IsBridge}}</p>
      </div>
      <span class="badge {{componentStateClass .Name .Version $.IsBridge}}">{{componentStateLabel .Name .Version $.IsBridge}}</span>
    </div>
    <div class="component-version mono">{{.Version}}</div>
  </article>
{{end}}
</div>
<div class="ops-inline-metrics">
  <div class="ops-inline-metric"><span class="k">Installed components</span><span class="v mono">{{len .Components}}</span></div>
  <div class="ops-inline-metric"><span class="k">Kernel</span><span class="v mono">{{.System.Kernel}}</span></div>
  <div class="ops-inline-metric"><span class="k">Load</span><span class="v mono">{{.System.LoadAvg}}</span></div>
  <div class="ops-inline-metric"><span class="k">Bridge Nodes</span><span class="v mono">{{countEnabledNodes .BridgeNodes}} / {{len .BridgeNodes}}</span></div>
</div>
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

func trafficLinePath(series []metrics.TrafficBucket) string {
	return trafficLinePathBy(series, func(bucket metrics.TrafficBucket) int64 {
		return bucket.BytesIn + bucket.BytesOut
	})
}

func trafficLinePathBy(series []metrics.TrafficBucket, value func(metrics.TrafficBucket) int64) string {
	points := trafficPoints(series, value)
	if len(points) == 0 {
		return ""
	}
	var b strings.Builder
	for i, point := range points {
		if i == 0 {
			fmt.Fprintf(&b, "M%.2f,%.2f", point.x, point.y)
			continue
		}
		fmt.Fprintf(&b, " L%.2f,%.2f", point.x, point.y)
	}
	return b.String()
}

func trafficAreaPath(series []metrics.TrafficBucket) string {
	return trafficAreaPathBy(series, func(bucket metrics.TrafficBucket) int64 {
		return bucket.BytesIn + bucket.BytesOut
	})
}

func trafficAreaPathBy(series []metrics.TrafficBucket, value func(metrics.TrafficBucket) int64) string {
	points := trafficPoints(series, value)
	if len(points) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "M%.2f,56.00", points[0].x)
	for _, point := range points {
		fmt.Fprintf(&b, " L%.2f,%.2f", point.x, point.y)
	}
	last := points[len(points)-1]
	fmt.Fprintf(&b, " L%.2f,56.00 Z", last.x)
	return b.String()
}

type svgPoint struct {
	x float64
	y float64
}

func trafficPoints(series []metrics.TrafficBucket, value func(metrics.TrafficBucket) int64) []svgPoint {
	if len(series) == 0 {
		return nil
	}
	maxValue := int64(0)
	for _, bucket := range series {
		current := value(bucket)
		if current > maxValue {
			maxValue = current
		}
	}
	if maxValue <= 0 {
		maxValue = 1
	}
	points := make([]svgPoint, 0, len(series))
	width := 100.0
	height := 56.0
	for i, bucket := range series {
		x := 0.0
		if len(series) > 1 {
			x = (float64(i) / float64(len(series)-1)) * width
		}
		current := value(bucket)
		ratio := float64(current) / float64(maxValue)
		y := height - math.Max(4, ratio*(height-8))
		points = append(points, svgPoint{x: x, y: y})
	}
	return points
}
