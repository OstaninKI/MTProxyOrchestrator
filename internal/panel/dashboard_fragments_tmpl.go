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
	"icon":         layoutIcon,
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
	"barValue": func(v float64) int {
		if v < 0 {
			return 0
		}
		if v > 100 {
			v = 100
		}
		return int(math.Round(v))
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
  class="card"
  hx-get="{{.PanelPath}}dashboard/fragments/health?period={{.Period}}"
  hx-trigger="sse:dashboard-health"
  hx-swap="outerHTML">
  <div class="card-head">
    <div class="col card-title-stack">
      <h3>{{if .IsBridge}}Bridge Mode — Chain Health{{else}}System{{end}}</h3>
      <span class="sub">{{if .IsBridge}}{{.HealthLabel}}{{else}}Live resource utilization{{end}}</span>
    </div>
  </div>
  <div class="card-body">
    <div class="resource-row">
      <span class="resource-icon">{{icon "Activity" 14}}</span>
      <span class="resource-label">Memory</span>
      <progress class="bar" data-tone="{{usageTone .System.MemoryPercent}}" value="{{barValue .System.MemoryPercent}}" max="100"></progress>
      <span class="mono tnum resource-value">{{formatPercent .System.MemoryPercent}}</span>
    </div>
    <div class="resource-row">
      <span class="resource-icon">{{icon "Server" 14}}</span>
      <span class="resource-label">Disk</span>
      <progress class="bar" data-tone="{{usageTone .System.DiskPercent}}" value="{{barValue .System.DiskPercent}}" max="100"></progress>
      <span class="mono tnum resource-value">{{formatPercent .System.DiskPercent}}</span>
    </div>
    <div class="divider"></div>
    <div class="resource-meta"><span>Load avg</span><span class="mono">{{.System.LoadAvg}}</span></div>
    <div class="resource-meta"><span>Uptime</span><span class="mono">{{.System.Uptime}}</span></div>
    <div class="resource-meta"><span>Kernel</span><span class="mono">{{.System.Kernel}}</span></div>
    <div class="resource-meta"><span>Mode</span><span class="mono">{{if .IsBridge}}Bridge Mode{{else}}Services{{end}}</span></div>
    {{if .IsBridge}}
    <div class="divider"></div>
    <div class="table-wrap table-wrap--plain"><table class="tbl tbl--compact">
      <thead><tr><th>Step</th><th>Status</th></tr></thead>
      <tbody>{{range .BridgeSteps}}<tr><td>{{.Name}}</td><td>{{if .OK}}<span class="badge" data-tone="success"><span class="dot"></span>ok</span>{{else}}<span class="badge" data-tone="danger"><span class="dot"></span>down</span>{{end}}</td></tr>{{end}}</tbody>
    </table></div>
    {{else if .Services}}
    <div class="divider"></div>
    <div class="table-wrap table-wrap--plain"><table class="tbl tbl--compact">
      <thead><tr><th>Service</th><th>Status</th><th>Message</th></tr></thead>
      <tbody>{{range .Services}}<tr><td>{{.Name}}</td><td>{{if .Active}}<span class="badge" data-tone="success"><span class="dot"></span>running</span>{{else}}<span class="badge" data-tone="danger"><span class="dot"></span>down</span>{{end}}</td><td>{{.Message}}</td></tr>{{end}}</tbody>
    </table></div>
    {{end}}
  </div>
</section>
{{end}}

{{define "connections"}}
<section
  id="dashboard-connections"
  class="card"
  hx-get="{{.PanelPath}}dashboard/fragments/connections?period={{.Period}}"
  hx-trigger="sse:dashboard-connections"
  hx-swap="outerHTML">
<div class="card-head"><div class="col card-title-stack"><h3>Active Connections</h3><span class="sub">{{len .LiveConnections}} users active now, {{len .Users}} configured in panel.</span></div><div class="spacer"></div><span class="badge" data-tone="success"><span class="dot"></span>{{sumLiveConnections .LiveConnections}} live</span></div>
<div class="card-body card-body--flush">
{{if .LiveConnections}}
<table class="tbl tbl--compact">
<thead><tr><th>User</th><th>Live</th></tr></thead>
<tbody>
{{range .LiveConnections}}
<tr>
  <td><span class="connection-user">{{.UserLabel}}</span></td>
  <td><span class="badge" data-tone="success"><span class="dot"></span>{{.Connections}}</span></td>
</tr>
{{end}}
</tbody>
</table>
{{else}}
<div class="empty">No active connections.</div>
{{end}}
</div>
</section>
{{end}}

{{define "traffic"}}
<section
  id="dashboard-traffic"
  class="card"
  hx-get="{{.PanelPath}}dashboard/fragments/traffic?period={{.Period}}"
  hx-trigger="sse:dashboard-traffic"
  hx-swap="outerHTML">
<div class="card-head">
  <div class="col card-title-stack"><h3>Network throughput</h3><span class="sub">Aggregate ingress / egress, selected window</span></div>
  <div class="spacer"></div>
  <div class="row">
    <div class="legend-dot"><span class="legend-download"></span><span>Download</span><strong class="mono">{{formatBytes (sumTrafficOutSeries .TrafficSeries)}}</strong></div>
    <div class="legend-dot"><span class="legend-upload"></span><span>Upload</span><strong class="mono">{{formatBytes (sumTrafficInSeries .TrafficSeries)}}</strong></div>
    <div class="seg">
      <a class="seg-item{{if eq .Period "1h"}} active{{end}}" href="?period=1h">1h</a>
      <a class="seg-item{{if eq .Period "24h"}} active{{end}}" href="?period=24h">24h</a>
      <a class="seg-item{{if eq .Period "7d"}} active{{end}}" href="?period=7d">7d</a>
      <a class="seg-item{{if eq .Period "30d"}} active{{end}}" href="?period=30d">30d</a>
    </div>
  </div>
</div>
<div class="card-body traffic-overview">
  {{if .TrafficSeries}}
    <svg class="traffic-chart area-chart" viewBox="0 0 100 56" preserveAspectRatio="none" aria-label="Network throughput chart">
      <path class="traffic-chart-area traffic-chart-area-out" d="{{trafficAreaPathOut .TrafficSeries}}"></path>
      <path class="traffic-chart-line traffic-chart-line-out" d="{{trafficLinePathOut .TrafficSeries}}"></path>
      <path class="traffic-chart-area traffic-chart-area-in" d="{{trafficAreaPathIn .TrafficSeries}}"></path>
      <path class="traffic-chart-line traffic-chart-line-in" d="{{trafficLinePathIn .TrafficSeries}}"></path>
    </svg>
  {{else}}
    <div class="empty">No traffic data for this period.</div>
  {{end}}
</div>
</section>
{{end}}

{{define "components"}}
<section
  id="dashboard-components"
  class="card"
  hx-get="{{.PanelPath}}dashboard/fragments/components?period={{.Period}}"
  hx-trigger="sse:dashboard-components"
  hx-swap="outerHTML">
<div class="card-head"><div class="col card-title-stack"><h3>Services & Components</h3><span class="sub">systemd units and installed binaries</span></div><div class="spacer"></div><button class="btn" data-variant="ghost" data-size="sm" disabled>{{icon "Refresh" 13}} Refresh</button></div>
<div class="card-body card-body--flush">
<table class="tbl tbl--compact">
  <thead><tr><th>Service</th><th>Version</th><th>Status</th><th>Detail</th><th class="text-right">Restarts</th><th></th></tr></thead>
  <tbody>
  {{range .Components}}
  <tr>
    <td><span class="mono">{{.Name}}</span></td>
    <td><span class="badge" data-tone="accent">{{.Version}}</span></td>
    <td><span class="badge {{componentStateClass .Name .Version $.IsBridge}}" data-tone="{{componentStateClass .Name .Version $.IsBridge}}"><span class="dot"></span>{{componentStateLabel .Name .Version $.IsBridge}}</span></td>
    <td class="muted">{{componentNote .Name $.IsBridge}}</td>
    <td class="mono text-right">0</td>
    <td class="actions-cell"><button class="btn" data-size="xs" data-variant="ghost" disabled>{{icon "Refresh" 12}} Restart</button></td>
  </tr>
  {{end}}
  {{range .Services}}
  <tr>
    <td><span class="mono">{{.Name}}</span></td>
    <td><span class="badge" data-tone="accent">systemd</span></td>
    <td>{{if .Active}}<span class="badge" data-tone="success"><span class="dot"></span>Running</span>{{else}}<span class="badge" data-tone="danger"><span class="dot"></span>Down</span>{{end}}</td>
    <td class="muted">{{.Message}}</td>
    <td class="mono text-right">0</td>
    <td></td>
  </tr>
  {{end}}
  </tbody>
</table>
</div>
</section>
{{end}}

{{define "top_users"}}
<section class="card">
  <div class="card-head"><div class="col card-title-stack"><h3>Top users by traffic</h3><span class="sub">Live · selected period</span></div><div class="spacer"></div><div class="seg"><a class="seg-item" href="?period=1h">1h</a><a class="seg-item active" href="?period=24h">24h</a><a class="seg-item" href="?period=7d">7d</a><a class="seg-item" href="?period=30d">30d</a></div></div>
  <div class="card-body card-body--flush">
  {{if .TopUsers}}
    <table class="tbl tbl--compact">
      <thead><tr><th>User</th><th>Activity</th><th class="text-right">Download</th><th class="text-right">Upload</th><th class="text-right">Total</th><th class="text-right">Conn</th></tr></thead>
      <tbody>{{range .TopUsers}}<tr><td><span class="traffic-user">{{.UserLabel}}</span></td><td><span class="muted">sampled</span></td><td class="mono text-right">{{formatBytes .BytesOut}}</td><td class="mono text-right">{{formatBytes .BytesIn}}</td><td class="mono text-right font-medium">{{formatBytes (trafficTotal .BytesIn .BytesOut)}}</td><td class="mono text-right">{{.Connections}}</td></tr>{{end}}</tbody>
    </table>
  {{else}}
    <div class="empty">No traffic data for this period.</div>
  {{end}}
  </div>
</section>
{{end}}

{{define "bridge_nodes"}}
<section class="card">
  <div class="card-head"><div class="col card-title-stack"><h3>Bridge nodes</h3><span class="sub">{{countEnabledNodes .BridgeNodes}} of {{len .BridgeNodes}} online</span></div></div>
  <div class="card-body card-body--flush">
  {{if .BridgeNodes}}
    <div class="node-list">
    {{range previewBridgeNodes .BridgeNodes 4}}
      <div class="node-row">
        <span class="badge {{bridgeNodeStateClass .}}" data-tone="{{bridgeNodeStateClass .}}"><span class="dot"></span>{{bridgeNodeStateLabel .}}</span>
        <div class="col col-zero col-fill"><span class="bridge-node-title">{{.Tag}}</span><span class="muted mono muted-sm">{{.Type}} · {{.Host}}</span></div>
        <span class="mono muted muted-sm">{{if .LastLatency}}{{.LastLatency}}ms{{else}}—{{end}}</span>
      </div>
    {{end}}
    </div>
    <div class="card-body card-body--tight"><a class="btn" data-size="sm" data-variant="ghost" href="{{.PanelPath}}bridge">Open Bridge</a></div>
  {{else}}
    <div class="empty">No Bridge nodes configured.</div>
  {{end}}
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
