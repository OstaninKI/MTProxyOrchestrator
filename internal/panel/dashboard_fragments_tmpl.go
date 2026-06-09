package panel

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"math"
	"strings"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
)

var dashboardFragmentFuncs = template.FuncMap{
	"csrfField":    layoutCSRFField,
	"csrfToken":    layoutCSRFToken,
	"navIsCurrent": layoutNavIsCurrent,
	"icon":         layoutIcon,
	"assetv":       assetVersion,
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
	"maxTrafficConnections": func(series []metrics.TrafficBucket) int64 {
		return trafficConnectionsMax(series)
	},
	"socks5SuccessPercent": func(c metrics.UpstreamCounters) string {
		if c.Attempted <= 0 {
			return "n/a"
		}
		return fmt.Sprintf("%.0f%%", (float64(c.Succeeded)/float64(c.Attempted))*100)
	},
	"rejectionRatePercent": func(snapshot metrics.Snapshot) string {
		total := snapshot.AcceptedConnectionsTotal + snapshot.RejectedConnectionsTotal
		if total <= 0 {
			return "n/a"
		}
		return fmt.Sprintf("%.1f%%", (float64(snapshot.RejectedConnectionsTotal)/float64(total))*100)
	},
	"dcLatencyLabel": func(d metrics.DCStat) string {
		if d.LastLatencyMs < 0 {
			return "n/a"
		}
		return fmt.Sprintf("%.0f ms", d.LastLatencyMs)
	},
	"dcTone": func(d metrics.DCStat) string {
		switch {
		case d.ProbeFailures > 0:
			return "danger"
		case d.LastLatencyMs < 0:
			return "neutral"
		case d.LastLatencyMs >= 500:
			return "danger"
		case d.LastLatencyMs >= 200:
			return "warn"
		default:
			return "success"
		}
	},
	"hasSecretLimits": func(sample metrics.Sample) bool {
		return sample.ConnectionLimit > 0 || sample.UniqueIPs > 0 || sample.MaxIPs > 0
	},
	"hasSecretRejections": func(sample metrics.Sample) bool {
		return sample.RejectedConnections > 0 || sample.RejectedQuota > 0 || sample.RejectedIPs > 0 || sample.RejectedExpired > 0
	},
	"previewJA4": func(rows []metrics.JA4Counter, limit int) []metrics.JA4Counter {
		if limit <= 0 || len(rows) <= limit {
			return rows
		}
		return rows[:limit]
	},
	"hasOperationalMetrics": func(snapshot metrics.Snapshot) bool {
		return snapshot.AcceptedConnectionsTotal > 0 ||
			snapshot.RejectedConnectionsTotal > 0 ||
			snapshot.SOCKS5.Attempted > 0 ||
			len(snapshot.JA4) > 0 ||
			len(snapshot.DCStats) > 0 ||
			snapshot.ProxyProtocol.Connections > 0 ||
			snapshot.ProxyProtocol.Errors > 0
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
	"trafficChartSVG": func(series []metrics.TrafficBucket) template.HTML {
		return trafficChartSVG(series)
	},
	"kpiSparkSVG": func(series []metrics.TrafficBucket) template.HTML {
		return kpiSparkSVG(series)
	},
	"opsHasTrend": opsHasTrend,
	"opsRateSparkSVG": func(series []metrics.OpsBucket, kind string) template.HTML {
		return opsRateSparkSVG(series, kind)
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
      <h3>System</h3>
      <span class="sub">Live resource utilization</span>
    </div>
  </div>
  <div class="card-body">
    <div class="resource-row">
      <span class="resource-icon">{{icon "Activity" 14}}</span>
      <span class="resource-label">Memory</span>
      <progress class="bar" data-tone="{{usageTone .System.MemoryPercent}}" value="{{barValue .System.MemoryPercent}}" max="100"></progress>
      <span class="mono tnum resource-value">{{formatPercent .System.MemoryPercent}}{{if gt .System.MemoryTotal 0}} / {{formatBytes .System.MemoryTotal}}{{end}}</span>
    </div>
    <div class="resource-row">
      <span class="resource-icon">{{icon "Server" 14}}</span>
      <span class="resource-label">Disk</span>
      <progress class="bar" data-tone="{{usageTone .System.DiskPercent}}" value="{{barValue .System.DiskPercent}}" max="100"></progress>
      <span class="mono tnum resource-value">{{formatPercent .System.DiskPercent}}{{if gt .System.DiskTotal 0}} / {{formatBytes .System.DiskTotal}}{{end}}</span>
    </div>
    <div class="divider"></div>
    <div class="resource-meta"><span>Load avg</span><span class="mono">{{.System.LoadAvg}}</span></div>
    <div class="resource-meta"><span>Uptime</span><span class="mono">{{.System.Uptime}}</span></div>
    <div class="resource-meta"><span>Kernel</span><span class="mono">{{.System.Kernel}}</span></div>
    <div class="resource-meta"><span>Mode</span><span class="mono">{{if .IsBridge}}Bridge Mode{{else}}Services{{end}}</span></div>
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
<thead><tr><th>User</th><th>Live</th><th class="text-right">Upload</th><th class="text-right">Download</th><th>Limits</th><th>Rejected</th></tr></thead>
<tbody>
{{range .LiveConnections}}
<tr>
  <td><span class="connection-user">{{.UserLabel}}</span></td>
  <td><span class="badge" data-tone="success"><span class="dot"></span>{{.Connections}}</span></td>
  <td class="mono text-right">{{formatBytes .BytesIn}}</td>
  <td class="mono text-right">{{formatBytes .BytesOut}}</td>
  <td>{{if hasSecretLimits .}}<span class="mono">{{.Connections}} / {{.ConnectionLimit}}</span>{{if or .UniqueIPs .MaxIPs}} <span class="muted">{{.UniqueIPs}} / {{.MaxIPs}} IPs</span>{{end}}{{else}}<span class="muted">unlimited</span>{{end}}</td>
  <td>{{if hasSecretRejections .}}<span class="badge" data-tone="warn">{{.RejectedConnections}}</span> <span class="muted">quota {{.RejectedQuota}} · IP {{.RejectedIPs}} · expired {{.RejectedExpired}}</span>{{else}}<span class="muted">none</span>{{end}}</td>
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

{{define "ops"}}
<section
  id="dashboard-ops"
  class="card"
  hx-get="{{.PanelPath}}dashboard/fragments/ops?period={{.Period}}"
  hx-trigger="sse:dashboard-ops"
  hx-swap="outerHTML">
<div class="card-head"><div class="col card-title-stack"><h3>Connection quality</h3><span class="sub">Accepted / rejected probes, SOCKS5 upstream and JA4 signals</span></div></div>
<div class="card-body">
  {{if hasOperationalMetrics .Teleproxy}}
  <div class="ops-inline-metrics">
    <div class="ops-inline-metric"><span class="k">Accepted</span><span class="v mono">{{.Teleproxy.AcceptedConnectionsTotal}}</span></div>
    <div class="ops-inline-metric"><span class="k">Rejected</span><span class="v mono">{{.Teleproxy.RejectedConnectionsTotal}}</span></div>
    <div class="ops-inline-metric"><span class="k">Reject rate</span><span class="v mono">{{rejectionRatePercent .Teleproxy}}</span></div>
    <div class="ops-inline-metric"><span class="k">SOCKS5 upstream</span><span class="v mono">{{socks5SuccessPercent .Teleproxy.SOCKS5}}</span></div>
  </div>
  {{if opsHasTrend .OpsSeries}}
  <div class="divider"></div>
  <div class="ops-trend">
    <div class="resource-meta"><span>Reject rate trend</span><span class="mono muted">{{.Period}} window</span></div>
    {{opsRateSparkSVG .OpsSeries "reject"}}
    <div class="resource-meta"><span>SOCKS5 success trend</span><span class="mono muted">{{.Period}} window</span></div>
    {{opsRateSparkSVG .OpsSeries "socks5"}}
  </div>
  {{end}}
  <div class="divider"></div>
  <div class="resource-meta"><span>SOCKS5 attempts</span><span class="mono">{{.Teleproxy.SOCKS5.Attempted}}</span></div>
  <div class="resource-meta"><span>SOCKS5 succeeded</span><span class="mono">{{.Teleproxy.SOCKS5.Succeeded}}</span></div>
  <div class="resource-meta"><span>SOCKS5 failed</span><span class="mono">{{.Teleproxy.SOCKS5.Failed}}</span></div>
  {{if or .Teleproxy.ProxyProtocol.Connections .Teleproxy.ProxyProtocol.Errors}}
  <div class="divider"></div>
  <div class="resource-meta"><span>PROXY-protocol conns</span><span class="mono">{{.Teleproxy.ProxyProtocol.Connections}}</span></div>
  <div class="resource-meta"><span>PROXY-protocol errors</span><span class="mono">{{.Teleproxy.ProxyProtocol.Errors}}</span></div>
  {{end}}
  <div class="divider"></div>
  <div class="resource-meta"><span>Security signals</span><span class="mono">{{len .Teleproxy.JA4}} JA4</span></div>
  {{if .Teleproxy.JA4}}
  <div class="node-list">
  {{range previewJA4 .Teleproxy.JA4 4}}
    <div class="node-row">
      <span class="badge" data-tone="accent">{{.Count}}</span>
      <span class="mono muted muted-sm">{{.Hash}}</span>
    </div>
  {{end}}
  </div>
  {{else}}
  <div class="empty">No JA4 fingerprints observed.</div>
  {{end}}
  {{else}}
  <div class="empty">No Teleproxy operational metrics yet.</div>
  {{end}}
</div>
</section>
{{end}}

{{define "upstream"}}
<section
  id="dashboard-upstream"
  class="card"
  hx-get="{{.PanelPath}}dashboard/fragments/upstream?period={{.Period}}"
  hx-trigger="sse:dashboard-upstream"
  hx-swap="outerHTML">
<div class="card-head"><div class="col card-title-stack"><h3>Telegram upstream health</h3><span class="sub">DC latency probes and probe failures</span></div></div>
<div class="card-body">
  {{if .Teleproxy.DCStats}}
  {{if .IsBridge}}
  <div class="note">DC probes run direct, bypassing the bridge — failures are expected in Bridge mode and do not reflect proxy health.</div>
  {{end}}
  {{range .Teleproxy.DCStats}}
  <div class="dc-latency-row">
    <span class="dc-label">DC {{.DC}}{{if .ProbeFailures}} <span class="dc-fail">· {{.ProbeFailures}} fail</span>{{end}}</span>
    <span class="mono"><span class="badge" data-tone="{{if $.IsBridge}}neutral{{else}}{{dcTone .}}{{end}}">{{dcLatencyLabel .}}</span></span>
  </div>
  {{end}}
  {{else}}
  <div class="empty">No DC probe data yet.</div>
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
  <div class="row traffic-toolbar">
    <div class="legend-dot"><span class="legend-download"></span><span>Download</span><strong class="mono">{{formatBytes (sumTrafficOutSeries .TrafficSeries)}}</strong></div>
    <div class="legend-dot"><span class="legend-upload"></span><span>Upload</span><strong class="mono">{{formatBytes (sumTrafficInSeries .TrafficSeries)}}</strong></div>
    <div class="legend-dot"><span class="legend-connections"></span><span>Connections</span><strong class="mono">{{maxTrafficConnections .TrafficSeries}}</strong></div>
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
    {{trafficChartSVG .TrafficSeries}}
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
    <td class="cell-name"><span class="mono">{{.Name}}</span></td>
    <td><span class="badge" data-tone="accent">{{.Version}}</span></td>
    <td><span class="badge {{componentStateClass .Name .Version $.IsBridge}}" data-tone="{{componentStateClass .Name .Version $.IsBridge}}"><span class="dot"></span>{{componentStateLabel .Name .Version $.IsBridge}}</span></td>
    <td class="muted cell-detail" title="{{componentNote .Name $.IsBridge}}">{{componentNote .Name $.IsBridge}}</td>
    <td class="mono text-right">0</td>
    <td class="actions-cell"><button class="btn" data-size="xs" data-variant="ghost" disabled>{{icon "Refresh" 12}} Restart</button></td>
  </tr>
  {{end}}
  {{range .Services}}
  <tr>
    <td class="cell-name"><span class="mono">{{.Name}}</span></td>
    <td><span class="badge" data-tone="accent">systemd</span></td>
    <td>{{if .Active}}<span class="badge" data-tone="success"><span class="dot"></span>Running</span>{{else}}<span class="badge" data-tone="danger"><span class="dot"></span>Down</span>{{end}}</td>
    <td class="muted cell-detail" title="{{.Message}}">{{.Message}}</td>
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
  <div class="card-head"><div class="col card-title-stack"><h3>Top users by traffic</h3><span class="sub">Live · selected period</span></div><div class="spacer"></div><div class="seg"><a class="seg-item{{if eq .Period "1h"}} active{{end}}" href="?period=1h">1h</a><a class="seg-item{{if eq .Period "24h"}} active{{end}}" href="?period=24h">24h</a><a class="seg-item{{if eq .Period "7d"}} active{{end}}" href="?period=7d">7d</a><a class="seg-item{{if eq .Period "30d"}} active{{end}}" href="?period=30d">30d</a></div></div>
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

func dashboardOpsFragment(w io.Writer, data DashboardData) {
	dashboardFragmentsTmpl.ExecuteTemplate(w, "ops", data) //nolint:errcheck
}

func dashboardUpstreamFragment(w io.Writer, data DashboardData) {
	dashboardFragmentsTmpl.ExecuteTemplate(w, "upstream", data) //nolint:errcheck
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

type trafficChartDims struct {
	width    float64
	height   float64
	leftPad  float64
	rightPad float64
	topPad   float64
	botPad   float64
}

func trafficChartSVG(series []metrics.TrafficBucket) template.HTML {
	if len(series) == 0 {
		return ""
	}

	dims := trafficChartDims{
		width:    760,
		height:   220,
		leftPad:  36,
		rightPad: 12,
		topPad:   10,
		botPad:   22,
	}
	maxValue := trafficSeriesMax(series)
	maxConnections := trafficConnectionsMax(series)
	outPoints := trafficPointsScaled(series, func(bucket metrics.TrafficBucket) int64 { return bucket.BytesOut }, maxValue, dims)
	inPoints := trafficPointsScaled(series, func(bucket metrics.TrafficBucket) int64 { return bucket.BytesIn }, maxValue, dims)
	connPoints := trafficPointsScaled(series, func(bucket metrics.TrafficBucket) int64 { return bucket.Connections }, maxConnections, dims)
	chartWidth := dims.width - dims.leftPad - dims.rightPad
	chartHeight := dims.height - dims.topPad - dims.botPad
	baselineY := dims.topPad + chartHeight

	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="traffic-chart area-chart" viewBox="0 0 %.0f %.0f" preserveAspectRatio="none" aria-label="Network throughput chart">`, dims.width, dims.height)
	b.WriteString(`<defs>
<linearGradient id="traffic-fill-out" x1="0" x2="0" y1="0" y2="1">
<stop offset="0%" stop-color="oklch(0.78 0.16 240)" stop-opacity="0.35"></stop>
<stop offset="100%" stop-color="oklch(0.78 0.16 240)" stop-opacity="0"></stop>
</linearGradient>
<linearGradient id="traffic-fill-in" x1="0" x2="0" y1="0" y2="1">
<stop offset="0%" stop-color="oklch(0.78 0.16 300)" stop-opacity="0.30"></stop>
<stop offset="100%" stop-color="oklch(0.78 0.16 300)" stop-opacity="0"></stop>
</linearGradient>
</defs>`)

	for i := 0; i <= 4; i++ {
		y := dims.topPad + (chartHeight*float64(i))/4
		value := int64(math.Round(float64(maxValue) * (1 - float64(i)/4)))
		fmt.Fprintf(&b, `<line class="traffic-grid-line" x1="%.2f" x2="%.2f" y1="%.2f" y2="%.2f"></line>`, dims.leftPad, dims.leftPad+chartWidth, y, y)
		fmt.Fprintf(&b, `<text x="%.2f" y="%.2f" text-anchor="end">%s</text>`, dims.leftPad-6, y+3, formatTrafficAxisValue(value))
	}

	fmt.Fprintf(&b, `<line class="traffic-axis-line" x1="%.2f" x2="%.2f" y1="%.2f" y2="%.2f"></line>`, dims.leftPad, dims.leftPad+chartWidth, baselineY, baselineY)
	for _, idx := range trafficXTicks(len(series), 6) {
		x := dims.leftPad
		if len(series) > 1 {
			x += (float64(idx) / float64(len(series)-1)) * chartWidth
		}
		fmt.Fprintf(&b, `<text x="%.2f" y="%.2f" text-anchor="middle">%s</text>`, x, dims.height-4, formatTrafficTimeLabel(series[idx].TS, series[0].TS, series[len(series)-1].TS))
	}

	fmt.Fprintf(&b, `<path class="traffic-chart-area traffic-chart-area-out" d="%s"></path>`, trafficAreaPathFromPoints(outPoints, baselineY))
	fmt.Fprintf(&b, `<path class="traffic-chart-line traffic-chart-line-out" d="%s"></path>`, trafficLinePathFromPoints(outPoints))
	fmt.Fprintf(&b, `<path class="traffic-chart-area traffic-chart-area-in" d="%s"></path>`, trafficAreaPathFromPoints(inPoints, baselineY))
	fmt.Fprintf(&b, `<path class="traffic-chart-line traffic-chart-line-in" d="%s"></path>`, trafficLinePathFromPoints(inPoints))
	fmt.Fprintf(&b, `<path class="traffic-chart-line traffic-chart-line-connections" d="%s"></path>`, trafficLinePathFromPoints(connPoints))

	// Hover overlay: a vertical crosshair plus a marker per series. Hidden until
	// the client JS positions it on pointer move. preserveAspectRatio="none" means
	// the viewBox X axis maps linearly to client pixels, so JS can convert a cursor
	// position to the nearest bucket without measuring the rendered geometry.
	fmt.Fprintf(&b, `<g class="traffic-hover" style="display:none">`+
		`<line class="traffic-hover-line" y1="%.2f" y2="%.2f"></line>`+
		`<circle class="traffic-hover-dot traffic-hover-dot-out" r="3.5"></circle>`+
		`<circle class="traffic-hover-dot traffic-hover-dot-in" r="3.5"></circle>`+
		`<circle class="traffic-hover-dot traffic-hover-dot-connections" r="3"></circle>`+
		`</g>`, dims.topPad, baselineY)
	b.WriteString(`</svg>`)

	// Per-bucket data the client uses to render the tooltip. Keys are kept short to
	// keep the attribute compact; coordinates are in viewBox units.
	points := make([]trafficHoverPoint, 0, len(series))
	for i, bucket := range series {
		points = append(points, trafficHoverPoint{
			X:    outPoints[i].x,
			YOut: outPoints[i].y,
			YIn:  inPoints[i].y,
			YCon: connPoints[i].y,
			Time: formatTrafficPointTime(bucket.TS, series[0].TS, series[len(series)-1].TS),
			Out:  formatBytes(uint64(maxNonNeg(bucket.BytesOut))),
			In:   formatBytes(uint64(maxNonNeg(bucket.BytesIn))),
			Con:  bucket.Connections,
		})
	}
	data, err := json.Marshal(points)
	if err != nil {
		data = []byte("[]")
	}

	var wrap strings.Builder
	fmt.Fprintf(&wrap, `<div class="traffic-chart-wrap" data-traffic-chart data-points="%s">`, template.HTMLEscapeString(string(data)))
	wrap.WriteString(b.String())
	wrap.WriteString(`<div class="traffic-tooltip" role="status" aria-live="polite" hidden></div>`)
	wrap.WriteString(`</div>`)

	return template.HTML(wrap.String())
}

// trafficHoverPoint is the per-bucket payload embedded in the chart's
// data-points attribute and consumed by the client tooltip code.
type trafficHoverPoint struct {
	X    float64 `json:"x"`
	YOut float64 `json:"yo"`
	YIn  float64 `json:"yi"`
	YCon float64 `json:"yc"`
	Time string  `json:"t"`
	Out  string  `json:"o"`
	In   string  `json:"i"`
	Con  int64   `json:"c"`
}

func maxNonNeg(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// formatTrafficPointTime renders a per-bucket timestamp for the hover tooltip.
// It includes the date when the window spans more than a day so multi-day
// windows stay unambiguous.
func formatTrafficPointTime(ts, first, last int64) string {
	if ts <= 0 {
		return "—"
	}
	t := time.Unix(ts, 0).UTC()
	if last-first >= int64(24*time.Hour/time.Second) {
		return t.Format("Jan 2 15:04")
	}
	return t.Format("15:04")
}

// kpiSparkSVG renders a compact background sparkline for a KPI tile from the
// per-bucket total (in+out) of a traffic series. Mirrors the design template's
// kpi-spark element.
func kpiSparkSVG(series []metrics.TrafficBucket) template.HTML {
	if len(series) < 2 {
		return ""
	}
	dims := trafficChartDims{width: 260, height: 36, leftPad: 0, rightPad: 0, topPad: 3, botPad: 3}
	totalMax := int64(1)
	for _, bucket := range series {
		if t := bucket.BytesIn + bucket.BytesOut; t > totalMax {
			totalMax = t
		}
	}
	points := trafficPointsScaled(series, func(bucket metrics.TrafficBucket) int64 {
		return bucket.BytesIn + bucket.BytesOut
	}, totalMax, dims)
	baselineY := dims.topPad + (dims.height - dims.topPad - dims.botPad)

	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="kpi-spark" viewBox="0 0 %.0f %.0f" preserveAspectRatio="none" aria-hidden="true">`, dims.width, dims.height)
	b.WriteString(`<defs><linearGradient id="kpi-spark-fill" x1="0" x2="0" y1="0" y2="1"><stop offset="0%" stop-color="oklch(0.78 0.16 240)" stop-opacity="0.35"></stop><stop offset="100%" stop-color="oklch(0.78 0.16 240)" stop-opacity="0"></stop></linearGradient></defs>`)
	fmt.Fprintf(&b, `<path d="%s" fill="url(#kpi-spark-fill)"></path>`, trafficAreaPathFromPoints(points, baselineY))
	fmt.Fprintf(&b, `<path d="%s" fill="none" stroke="oklch(0.78 0.16 240)" stroke-width="1.5"></path>`, trafficLinePathFromPoints(points))
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

// opsRateValues maps an operational series to a 0–100 percentage trend.
// kind is "reject" (rejected / accepted+rejected) or "socks5" (succeeded / attempted).
func opsRateValues(series []metrics.OpsBucket, kind string) []float64 {
	out := make([]float64, 0, len(series))
	for _, b := range series {
		switch kind {
		case "reject":
			total := b.Accepted + b.Rejected
			if total <= 0 {
				out = append(out, 0)
				continue
			}
			out = append(out, float64(b.Rejected)/float64(total)*100)
		case "socks5":
			if b.SOCKS5Attempted <= 0 {
				out = append(out, 0)
				continue
			}
			out = append(out, float64(b.SOCKS5Succeeded)/float64(b.SOCKS5Attempted)*100)
		}
	}
	return out
}

// opsHasTrend reports whether the series carries enough activity to chart.
func opsHasTrend(series []metrics.OpsBucket) bool {
	if len(series) < 2 {
		return false
	}
	for _, b := range series {
		if b.Accepted > 0 || b.Rejected > 0 || b.SOCKS5Attempted > 0 {
			return true
		}
	}
	return false
}

// opsRateSparkSVG renders a compact 0–100% trend sparkline for an operational
// rate, reusing the traffic sparkline path builders for visual consistency with
// the rest of the dashboard. hue selects the accent (warm for rejects, green
// for SOCKS5 success), matching the oklch palette used elsewhere.
func opsRateSparkSVG(series []metrics.OpsBucket, kind string) template.HTML {
	values := opsRateValues(series, kind)
	if len(values) < 2 {
		return ""
	}
	hue := 25.0
	if kind == "socks5" {
		hue = 150.0
	}

	const width, height = 260.0, 36.0
	const top, bot = 3.0, 3.0
	chartH := height - top - bot
	baselineY := top + chartH
	n := len(values)
	pts := make([]svgPoint, n)
	for i, v := range values {
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		x := 0.0
		if n > 1 {
			x = (float64(i) / float64(n-1)) * width
		}
		pts[i] = svgPoint{x: x, y: top + (1-v/100)*chartH}
	}

	stroke := fmt.Sprintf("oklch(0.78 0.16 %.0f)", hue)
	gradID := fmt.Sprintf("ops-spark-fill-%s", kind)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="ops-spark" viewBox="0 0 %.0f %.0f" preserveAspectRatio="none" aria-hidden="true">`, width, height)
	fmt.Fprintf(&b, `<defs><linearGradient id="%s" x1="0" x2="0" y1="0" y2="1"><stop offset="0%%" stop-color="%s" stop-opacity="0.30"></stop><stop offset="100%%" stop-color="%s" stop-opacity="0"></stop></linearGradient></defs>`, gradID, stroke, stroke)
	fmt.Fprintf(&b, `<path d="%s" fill="url(#%s)"></path>`, trafficAreaPathFromPoints(pts, baselineY), gradID)
	fmt.Fprintf(&b, `<path d="%s" fill="none" stroke="%s" stroke-width="1.5"></path>`, trafficLinePathFromPoints(pts), stroke)
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

func trafficSeriesMax(series []metrics.TrafficBucket) int64 {
	maxValue := int64(1)
	for _, bucket := range series {
		if bucket.BytesIn > maxValue {
			maxValue = bucket.BytesIn
		}
		if bucket.BytesOut > maxValue {
			maxValue = bucket.BytesOut
		}
	}
	return maxValue
}

func trafficConnectionsMax(series []metrics.TrafficBucket) int64 {
	maxValue := int64(1)
	for _, bucket := range series {
		if bucket.Connections > maxValue {
			maxValue = bucket.Connections
		}
	}
	return maxValue
}

func trafficXTicks(length, count int) []int {
	if length <= 0 {
		return nil
	}
	if count <= 1 || length == 1 {
		return []int{0}
	}
	seen := make(map[int]struct{}, count)
	out := make([]int, 0, count)
	for i := 0; i < count; i++ {
		idx := int(math.Round((float64(i) / float64(count-1)) * float64(length-1)))
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		out = append(out, idx)
	}
	return out
}

func formatTrafficAxisValue(value int64) string {
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.1fG", float64(value)/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%dk", int(math.Round(float64(value)/1_000)))
	default:
		return fmt.Sprintf("%d", value)
	}
}

func formatTrafficTimeLabel(ts, first, last int64) string {
	if ts <= 0 {
		return "—"
	}
	t := time.Unix(ts, 0).UTC()
	span := last - first
	switch {
	case span >= int64(7*24*time.Hour/time.Second):
		return t.Format("Jan 2")
	case span >= int64(24*time.Hour/time.Second):
		return t.Format("Jan 2")
	default:
		return t.Format("15:04")
	}
}

func trafficPointsScaled(series []metrics.TrafficBucket, value func(metrics.TrafficBucket) int64, maxValue int64, dims trafficChartDims) []svgPoint {
	if len(series) == 0 {
		return nil
	}
	if maxValue <= 0 {
		maxValue = 1
	}
	chartWidth := dims.width - dims.leftPad - dims.rightPad
	chartHeight := dims.height - dims.topPad - dims.botPad
	points := make([]svgPoint, 0, len(series))
	for i, bucket := range series {
		x := dims.leftPad
		if len(series) > 1 {
			x += (float64(i) / float64(len(series)-1)) * chartWidth
		}
		ratio := float64(value(bucket)) / float64(maxValue)
		y := dims.topPad + chartHeight - (ratio * chartHeight)
		points = append(points, svgPoint{x: x, y: y})
	}
	return points
}

func trafficLinePathFromPoints(points []svgPoint) string {
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

func trafficAreaPathFromPoints(points []svgPoint, baselineY float64) string {
	if len(points) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "M%.2f,%.2f", points[0].x, baselineY)
	for _, point := range points {
		fmt.Fprintf(&b, " L%.2f,%.2f", point.x, point.y)
	}
	last := points[len(points)-1]
	fmt.Fprintf(&b, " L%.2f,%.2f Z", last.x, baselineY)
	return b.String()
}

// userSparkSVG renders a compact sparkline for the Users table Activity column.
// If len(series) < 2, renders a flat placeholder line. Otherwise scales and renders
// the per-bucket total (BytesIn+BytesOut) within a 120x28 viewBox.
func userSparkSVG(series []metrics.TrafficBucket, online bool) template.HTML {
	var classStr string
	if online {
		classStr = `class="user-spark is-online"`
	} else {
		classStr = `class="user-spark"`
	}

	// Flat line for no or minimal data
	if len(series) < 2 {
		return template.HTML(fmt.Sprintf(`<svg %s width="120" height="28" viewBox="0 0 120 28" aria-hidden="true" preserveAspectRatio="none"><path d="M0 22 L120 22"></path></svg>`, classStr))
	}

	// Scale the series to the 120x28 viewBox
	dims := trafficChartDims{width: 120, height: 28, leftPad: 0, rightPad: 0, topPad: 3, botPad: 3}
	totalMax := int64(1)
	for _, bucket := range series {
		if t := bucket.BytesIn + bucket.BytesOut; t > totalMax {
			totalMax = t
		}
	}
	points := trafficPointsScaled(series, func(bucket metrics.TrafficBucket) int64 {
		return bucket.BytesIn + bucket.BytesOut
	}, totalMax, dims)

	var b strings.Builder
	fmt.Fprintf(&b, `<svg %s width="120" height="28" viewBox="0 0 120 28" aria-hidden="true" preserveAspectRatio="none">`, classStr)
	fmt.Fprintf(&b, `<path d="%s"></path>`, trafficLinePathFromPoints(points))
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
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
