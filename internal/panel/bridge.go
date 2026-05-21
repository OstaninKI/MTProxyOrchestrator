package panel

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/component"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/component/versions"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/singbox"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
)

const systemctlTimeout = 10 * time.Second

func systemctlRun(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", args...).Run()
}

// BridgeConfig holds panel-level settings for Bridge mode operations.
// Populated from installation config; injected via Server.BridgeCfg.
type BridgeConfig struct {
	Paths       config.InstallPaths
	MTProtoPort int
	MaskHost    string
	StatsPort   int
}

// handleBridgePage renders the Bridge mode management page.
func (s *Server) handleBridgePage(w http.ResponseWriter, r *http.Request) {
	nl, _ := bridge.Load(s.nodePath())
	csrfToken, _ := NewCSRFToken()
	SetCSRFCookie(w, csrfToken, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	bridgePage(w, bridgePageData{
		CSRFField: CSRFField(),
		CSRFToken: csrfToken,
		Nodes:     nl.Nodes,
		Strategy:  nl.Strategy,
		PanelPath: s.PanelPath,
	})
}

// handleBridgeEnable processes the "Enable Bridge" form.
// Form fields: vless_url (required).
func (s *Server) handleBridgeEnable(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	rawURL := r.FormValue("vless_url")
	node, err := bridge.ImportVLESS(rawURL)
	if err != nil {
		http.Error(w, "invalid vless URL", http.StatusBadRequest)
		return
	}

	users, err := UserRepo{DB: s.DB}.List()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var entries []teleproxy.UserEntry
	for _, u := range users {
		if u.Enabled && u.DeletedAt == nil {
			entries = append(entries, teleproxy.UserEntry{Label: u.Label, Secret: u.SecretHex})
		}
	}

	paths := s.bridgePaths()
	svc := &bridge.BridgeService{
		Exec:     realBridgeExecutor{},
		NodePath: paths.OutboundsJSON,
	}
	enableCfg := bridge.EnableConfig{
		Node:           node,
		Paths:          paths,
		TeleproxyUsers: entries,
		MTProtoPort:    s.bridgeMTProtoPort(),
		MaskHost:       s.bridgeMaskHost(),
		StatsPort:      s.bridgeStatsPort(),
		SingboxURL:     singboxDownloadURL(),
		SingboxSHA256:  singboxDownloadSHA256(),
	}
	if err := svc.Enable(enableCfg); err != nil {
		http.Error(w, "bridge enable failed", http.StatusInternalServerError)
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "bridge.enable", node.Tag, "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, "bridge", http.StatusSeeOther)
}

// handleBridgeDisable stops sing-box and returns Teleproxy to Single mode.
func (s *Server) handleBridgeDisable(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	users, err := UserRepo{DB: s.DB}.List()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var entries []teleproxy.UserEntry
	for _, u := range users {
		if u.Enabled && u.DeletedAt == nil {
			entries = append(entries, teleproxy.UserEntry{Label: u.Label, Secret: u.SecretHex})
		}
	}

	paths := s.bridgePaths()
	svc := &bridge.BridgeService{
		Exec:     realBridgeExecutor{},
		NodePath: paths.OutboundsJSON,
	}
	disableCfg := bridge.DisableConfig{
		Paths:          paths,
		TeleproxyUsers: entries,
		MTProtoPort:    s.bridgeMTProtoPort(),
		MaskHost:       s.bridgeMaskHost(),
		StatsPort:      s.bridgeStatsPort(),
	}
	if err := svc.Disable(disableCfg); err != nil {
		http.Error(w, "bridge disable failed", http.StatusInternalServerError)
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "bridge.disable", "", "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, "bridge", http.StatusSeeOther)
}

// bridgePaths returns InstallPaths from Server.BridgeCfg if set, else DefaultPaths.
func (s *Server) bridgePaths() config.InstallPaths {
	paths := config.DefaultPaths()
	if s.BridgeCfg == nil {
		return paths
	}
	override := s.BridgeCfg.Paths
	if override.ConfigDir != "" {
		paths.ConfigDir = override.ConfigDir
	}
	if override.LogDir != "" {
		paths.LogDir = override.LogDir
	}
	if override.BinDir != "" {
		paths.BinDir = override.BinDir
	}
	if override.SystemdDir != "" {
		paths.SystemdDir = override.SystemdDir
	}
	if override.StubDir != "" {
		paths.StubDir = override.StubDir
	}
	if override.CertDir != "" {
		paths.CertDir = override.CertDir
	}
	if override.NginxSnippetDir != "" {
		paths.NginxSnippetDir = override.NginxSnippetDir
	}
	if override.ConfigFile != "" {
		paths.ConfigFile = override.ConfigFile
	}
	if override.TeleproxyTOML != "" {
		paths.TeleproxyTOML = override.TeleproxyTOML
	}
	if override.SingboxJSON != "" {
		paths.SingboxJSON = override.SingboxJSON
	}
	if override.UsersJSON != "" {
		paths.UsersJSON = override.UsersJSON
	}
	if override.OutboundsJSON != "" {
		paths.OutboundsJSON = override.OutboundsJSON
	}
	if override.PanelDB != "" {
		paths.PanelDB = override.PanelDB
	}
	if override.PanelLog != "" {
		paths.PanelLog = override.PanelLog
	}
	if override.TeleproxyLog != "" {
		paths.TeleproxyLog = override.TeleproxyLog
	}
	if override.SingboxLog != "" {
		paths.SingboxLog = override.SingboxLog
	}
	if override.NginxLog != "" {
		paths.NginxLog = override.NginxLog
	}
	if override.TeleproxyBin != "" {
		paths.TeleproxyBin = override.TeleproxyBin
	}
	if override.SingboxBin != "" {
		paths.SingboxBin = override.SingboxBin
	}
	if override.CLIBin != "" {
		paths.CLIBin = override.CLIBin
	}
	if override.PanelBin != "" {
		paths.PanelBin = override.PanelBin
	}
	if override.TeleproxyService != "" {
		paths.TeleproxyService = override.TeleproxyService
	}
	if override.SingboxService != "" {
		paths.SingboxService = override.SingboxService
	}
	if override.PanelService != "" {
		paths.PanelService = override.PanelService
	}
	return paths
}

func (s *Server) bridgeMTProtoPort() int {
	if s.BridgeCfg != nil && s.BridgeCfg.MTProtoPort != 0 {
		return s.BridgeCfg.MTProtoPort
	}
	return 443
}

func (s *Server) bridgeMaskHost() string {
	if s.BridgeCfg != nil && s.BridgeCfg.MaskHost != "" {
		return s.BridgeCfg.MaskHost
	}
	return "www.microsoft.com"
}

func (s *Server) bridgeStatsPort() int {
	if s.BridgeCfg != nil && s.BridgeCfg.StatsPort != 0 {
		return s.BridgeCfg.StatsPort
	}
	return 9091
}

func (s *Server) nodePath() string {
	return filepath.Join(s.bridgePaths().OutboundsJSON)
}

// singboxIsActive checks if the sing-box service is running.
// Uses the injected SingboxActive func if set (for tests), otherwise checks systemd.
func (s *Server) singboxIsActive() bool {
	if s.SingboxActive != nil {
		return s.SingboxActive()
	}
	return systemctlRun("is-active", "sing-box.service") == nil
}

// realBridgeExecutor implements bridge.Executor using real OS calls.
type realBridgeExecutor struct{}

var rerenderSingboxIfActiveFn = func(s *Server, nl bridge.NodeList) error {
	return s.rerenderSingboxIfActive(nl)
}

func (realBridgeExecutor) WriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

func (realBridgeExecutor) Download(url, sha256hex, destPath string) error {
	d := component.Downloader{}
	if strings.HasSuffix(url, ".tar.gz") {
		return d.DownloadTarGzBinary(url, sha256hex, "sing-box", destPath)
	}
	return d.Download(url, sha256hex, destPath)
}

func singboxDownloadURL() string {
	return versions.SingboxLinuxAMD64URL
}

func singboxDownloadSHA256() string {
	return versions.SingboxLinuxAMD64SHA256
}

func (realBridgeExecutor) EnableService(name string) error {
	return systemctlRun("enable", name)
}

func (realBridgeExecutor) StartService(name string) error {
	if err := systemctlRun("restart", name); err != nil {
		return err
	}
	return verifyServiceHealthy(name)
}

func (realBridgeExecutor) StopService(name string) error {
	return systemctlRun("stop", name)
}

func (realBridgeExecutor) DisableService(name string) error {
	return systemctlRun("disable", name)
}

func (realBridgeExecutor) ReloadService(name string) error {
	return systemctlRun("reload-or-restart", name)
}

func (realBridgeExecutor) ServiceActive(name string) (bool, error) {
	return systemctlRun("is-active", name) == nil, nil
}

// verifyServiceHealthy polls is-failed/is-active for up to 5s after a restart
// to catch services that crash shortly after entering the activating state.
func verifyServiceHealthy(name string) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if systemctlRun("is-failed", "--quiet", name) == nil {
			return fmt.Errorf("service %s is in failed state after restart", name)
		}
		if systemctlRun("is-active", "--quiet", name) == nil && time.Now().After(deadline.Add(-3*time.Second)) {
			return nil
		}
		if time.Now().After(deadline) {
			if systemctlRun("is-active", "--quiet", name) == nil {
				return nil
			}
			return fmt.Errorf("service %s did not become active within 5s", name)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func cloneNodeList(nl bridge.NodeList) bridge.NodeList {
	clone := nl
	clone.Nodes = append([]bridge.Node(nil), nl.Nodes...)
	return clone
}

func saveNodeListWithRerenderRollback(s *Server, current bridge.NodeList, updated bridge.NodeList) error {
	nodePath := s.nodePath()
	if err := updated.Save(nodePath); err != nil {
		return err
	}
	if err := rerenderSingboxIfActiveFn(s, updated); err != nil {
		if restoreErr := current.Save(nodePath); restoreErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, restoreErr)
		}
		return err
	}
	return nil
}

// --- templates ---

type bridgePageData struct {
	CSRFField  string
	CSRFToken  string
	Nodes      []bridge.Node
	Strategy   string
	Flash      string // optional informational message
	CurrentNav string
	PanelPath  string
}

var bridgeTemplateFuncs = template.FuncMap{
	"countEnabledBridgeNodes": func(nodes []bridge.Node) int {
		count := 0
		for _, node := range nodes {
			if node.Enabled {
				count++
			}
		}
		return count
	},
	"countTestedBridgeNodes": func(nodes []bridge.Node) int {
		count := 0
		for _, node := range nodes {
			if node.LastLatency > 0 {
				count++
			}
		}
		return count
	},
	"avgBridgeLatency": func(nodes []bridge.Node) int {
		var sum, count int
		for _, node := range nodes {
			if node.LastLatency > 0 {
				sum += int(node.LastLatency)
				count++
			}
		}
		if count == 0 {
			return 0
		}
		return sum / count
	},
	"bridgeTypeTone": func(nodeType any) string {
		switch fmt.Sprint(nodeType) {
		case "vless-reality":
			return "ok"
		case "trojan", "hysteria2":
			return "warn bridge-badge-warn"
		case "shadowsocks", "tuic":
			return "bridge-badge-neutral"
		default:
			return "bridge-badge-neutral"
		}
	},
	"bridgeStatusTone": func(enabled bool) string {
		if enabled {
			return "ok"
		}
		return "down"
	},
	"bridgeLatencyTone": func(latency int64) string {
		switch {
		case latency <= 0:
			return "neutral"
		case latency <= 150:
			return "success"
		case latency <= 300:
			return "warn"
		default:
			return "danger"
		}
	},
	"bridgeLatencyPct": func(latency int64) int {
		switch {
		case latency <= 0:
			return 0
		case latency <= 100:
			return 92
		case latency <= 150:
			return 78
		case latency <= 250:
			return 58
		case latency <= 350:
			return 38
		default:
			return 22
		}
	},
}

var bridgeTmpl = layoutTemplate("bridge", `{{define "page_title"}}Bridge Mode{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Routing</p>
    <h1 class="page-title">Bridge Mode</h1>
    <p class="page-sub">Manage outbound nodes, routing strategy, and the switch between Single and Bridge mode.</p>
  </div>
  <div class="actions">
    <a class="page-cta" href="#add-node">Add node</a>
    <nav class="page-nav" aria-label="Bridge navigation">
      <a href="{{.PanelPath}}dashboard">Dashboard</a>
      <a href="{{.PanelPath}}settings/proxy">Proxy settings</a>
    </nav>
  </div>
</section>
<section class="page-stack">
{{if .Flash}}<div class="flash">{{.Flash}}</div>{{end}}
<section class="bridge-banner">
  <div class="summary-grid">
    <article class="summary-card">
      <span class="summary-label">Nodes total</span>
      <strong class="summary-value mono">{{len .Nodes}}</strong>
      <span class="summary-note">Configured outbounds</span>
    </article>
    <article class="summary-card">
      <span class="summary-label">Enabled</span>
      <strong class="summary-value mono">{{countEnabledBridgeNodes .Nodes}}</strong>
      <span class="summary-note">Eligible for routing</span>
    </article>
    <article class="summary-card">
      <span class="summary-label">Latency tested</span>
      <strong class="summary-value mono">{{countTestedBridgeNodes .Nodes}}</strong>
      <span class="summary-note">Nodes with measured RTT</span>
    </article>
    <article class="summary-card">
      <span class="summary-label">Avg latency</span>
      <strong class="summary-value mono">{{if gt (avgBridgeLatency .Nodes) 0}}{{avgBridgeLatency .Nodes}}ms{{else}}—{{end}}</strong>
      <span class="summary-note">{{if .Strategy}}{{.Strategy}}{{else}}urltest{{end}} strategy</span>
    </article>
  </div>
  <nav class="settings-tabs" aria-label="Bridge sections">
    <a class="active" href="#nodes">Nodes</a>
    <a href="#add-node">Add node</a>
    <a href="#routing-strategy">Routing</a>
    <a href="#mode-control">Mode control</a>
  </nav>
</section>

<div id="add-node" class="card form-panel">
<h2>Add Outbound Node via Share URL</h2>
<form method="post" action="{{.PanelPath}}bridge/nodes/add" class="bridge-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Share URL (vless://, trojan://, ss://, hysteria2://, tuic://)</label>
<input type="text" name="share_url" placeholder="vless://uuid@host:port?...#tag" required>
<button type="submit">Add Node</button>
</form>
</div>

<details class="card disclosure">
<summary>Add Node Manually</summary>
<form method="post" action="{{.PanelPath}}bridge/nodes/add-manual" class="bridge-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Protocol</label>
<select name="protocol" required>
  <option value="vless-reality">VLESS Reality</option>
  <option value="trojan">Trojan</option>
  <option value="shadowsocks">Shadowsocks</option>
  <option value="hysteria2">Hysteria2</option>
  <option value="tuic">TUIC</option>
</select>
<label>Tag (name)</label>
<input type="text" name="tag" placeholder="my-node" required>
<label>Host</label>
<input type="text" name="host" placeholder="1.2.3.4 or hostname" required>
<label>Port</label>
<input type="number" name="port" placeholder="443" min="1" max="65535" required>
<label>UUID (VLESS, TUIC)</label>
<input type="text" name="uuid" placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx">
<label>Password (Trojan, SS, Hysteria2, TUIC)</label>
<input type="text" name="password" placeholder="password">
<label>SNI (VLESS Reality, Trojan, Hysteria2, TUIC)</label>
<input type="text" name="sni" placeholder="example.com">
<label>Public Key (VLESS Reality)</label>
<input type="text" name="public_key" placeholder="base64 public key">
<label>Short ID (VLESS Reality, may be empty)</label>
<input type="text" name="short_id" placeholder="">
<label>Flow (VLESS Reality, optional)</label>
<input type="text" name="flow" placeholder="xtls-rprx-vision">
<label>Method/Cipher (Shadowsocks)</label>
<input type="text" name="method" placeholder="chacha20-ietf-poly1305">
<label>Congestion Control (TUIC, default: bbr)</label>
<input type="text" name="congestion_control" placeholder="bbr">
<button type="submit">Add Node Manually</button>
</form>
</details>

{{if .Nodes}}
<div id="nodes" class="card table-card">
<div class="card-body">
<h2>Outbound Nodes</h2>
<p class="panel-note">Status reflects whether the node is enabled in the Bridge config. Latency is shown only after a test run.</p>
</div>
<div class="table-wrap"><table>
<thead><tr><th>Tag</th><th>Type</th><th>Host</th><th>Port</th><th>Status</th><th>Latency</th><th>Actions</th></tr></thead>
<tbody>
{{range .Nodes}}
<tr>
  <td>
    <strong>{{.Tag}}</strong>
    {{if .SNI}}<div class="panel-note">{{.SNI}}</div>{{end}}
  </td>
  <td><span class="badge {{bridgeTypeTone .Type}}">{{.Type}}</span></td>
  <td>{{.Host}}</td>
  <td>{{.Port}}</td>
  <td><span class="badge {{bridgeStatusTone .Enabled}}">{{if .Enabled}}enabled{{else}}disabled{{end}}</span></td>
  <td>
    {{if .LastLatency}}
      <div class="bridge-latency">
        <span class="mono">{{.LastLatency}}ms</span>
        <div class="ops-meter bridge-latency-meter" data-tone="{{bridgeLatencyTone .LastLatency}}">
          <span style="width: {{bridgeLatencyPct .LastLatency}}%"></span>
        </div>
      </div>
    {{else}}
      <span class="muted">—</span>
    {{end}}
  </td>
  <td>
    <div class="bridge-actions">
    <form method="post" action="{{$.PanelPath}}bridge/nodes/{{.ID}}/toggle" class="inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit" class="btn-warn">{{if .Enabled}}Disable{{else}}Enable{{end}}</button>
    </form>
    <form method="post" action="{{$.PanelPath}}bridge/nodes/{{.ID}}/ping" class="inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">Test Latency</button>
    </form>
    <a href="{{$.PanelPath}}bridge/nodes/{{.ID}}/edit"><button type="button">Edit</button></a>
    <form method="post" action="{{$.PanelPath}}bridge/nodes/{{.ID}}/delete" class="inline"
          onsubmit="return confirm('Delete node {{.Tag}}?')">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit" class="danger">Delete</button>
    </form>
    </div>
  </td>
</tr>
{{end}}
</tbody>
</table></div>
</div>

<div id="routing-strategy" class="card form-panel">
<h2>Routing Strategy</h2>
<form method="post" action="{{.PanelPath}}bridge/strategy" class="bridge-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<select name="strategy">
  <option value="urltest"{{if eq .Strategy "urltest"}} selected{{end}}{{if eq .Strategy ""}} selected{{end}}>urltest (auto — lowest latency)</option>
  <option value="fallback"{{if eq .Strategy "fallback"}} selected{{end}}>fallback (primary + fallback)</option>
  <option value="roundrobin"{{if eq .Strategy "roundrobin"}} selected{{end}}>round-robin (rotate through nodes)</option>
  <option value="selector"{{if eq .Strategy "selector"}} selected{{end}}>selector (manual)</option>
</select>
<button type="submit">Save Strategy</button>
</form>
</div>

<div id="mode-control" class="card form-panel">
<h2>Mode Control</h2>
<form method="post" action="{{.PanelPath}}bridge/enable" class="bridge-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Enable Bridge with share URL</label>
<input type="text" name="vless_url" placeholder="vless://...#tag (VLESS Reality only for first enable)">
<button type="submit">Enable Bridge</button>
</form>
<form method="post" action="{{.PanelPath}}bridge/disable" class="bridge-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<button type="submit" class="danger">Disable Bridge (return to Single)</button>
</form>
</div>
{{end}}
</section>
{{end}}
{{template "base" .}}`, bridgeTemplateFuncs)

func bridgePage(w io.Writer, data bridgePageData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "bridge"
	}
	bridgeTmpl.Execute(w, data) //nolint:errcheck
}

// editNodeTmpl is the template for the node edit form.
var editNodeTmpl = layoutTemplate("editNode", `{{define "page_title"}}Edit Node{{end}}
{{define "content"}}
<section class="page-head">
  <div class="titles">
    <p class="page-eyebrow">Routing</p>
    <h1 class="page-title">Edit Node: {{.Node.Tag}}</h1>
    <p class="page-sub">Update visible routing fields. Secret material is intentionally not shown here.</p>
  </div>
  <nav class="page-nav" aria-label="Edit node navigation">
    <a href="{{.PanelPath}}bridge">Back to bridge</a>
    <a href="{{.PanelPath}}dashboard">Dashboard</a>
  </nav>
</section>
<div class="card form-panel">
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="post" action="" class="stack-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Tag (name)</label>
<input type="text" name="tag" value="{{.Node.Tag}}" required>
<label>Host</label>
<input type="text" name="host" value="{{.Node.Host}}" required>
<label>Port</label>
<input type="number" name="port" value="{{.Node.Port}}" min="1" max="65535" required>
<label>SNI</label>
<input type="text" name="sni" value="{{.Node.SNI}}">
<label>Flow (VLESS Reality)</label>
<input type="text" name="flow" value="{{.Node.Flow}}">
<label>Method/Cipher (Shadowsocks)</label>
<input type="text" name="method" value="{{.Node.Method}}">
<label>Congestion Control (TUIC)</label>
<input type="text" name="congestion_control" value="{{.Node.CongestionControl}}">
<p class="field-hint">Credentials (UUID, password, public key, short ID) are not shown and cannot be changed here. Delete and re-add the node to change credentials.</p>
<button type="submit">Save</button>
</form>
</div>
{{end}}
{{template "base" .}}`, nil)

type editNodePageData struct {
	CSRFField  string
	CSRFToken  string
	Node       bridge.Node
	Error      string
	CurrentNav string
	PanelPath  string
}

func editNodePage(w io.Writer, data editNodePageData) {
	if data.CurrentNav == "" {
		data.CurrentNav = "bridge"
	}
	editNodeTmpl.Execute(w, data) //nolint:errcheck
}

// handleBridgeAddNode adds a node via any supported share URL and re-renders
// the sing-box config if Bridge is currently active.
// Form field: share_url (required) — any of vless://, trojan://, ss://, hysteria2://, hy2://, tuic://
func (s *Server) handleBridgeAddNode(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	rawURL := r.FormValue("share_url")
	node, err := bridge.Import(rawURL)
	if err != nil {
		// Return a generic message; do not echo the raw URL or its credentials.
		http.Error(w, "invalid share URL: could not parse the provided URL", http.StatusBadRequest)
		return
	}

	nodePath := s.nodePath()
	nl, err := bridge.Load(nodePath)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	node.ID = nl.NextID()
	node.Enabled = true
	nl.Nodes = append(nl.Nodes, node)
	if err := nl.Save(nodePath); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := s.rerenderSingboxIfActive(nl); err != nil {
		// Rollback: remove the just-added node.
		nl.Nodes = nl.Nodes[:len(nl.Nodes)-1]
		_ = nl.Save(nodePath)
		http.Error(w, "node added but sing-box config could not be updated: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Log tag and type only — never password, uuid, or key material.
	audit.Log(s.DB, s.sessionAdminID(r), "node.add", node.Tag, string(node.Type), clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, "../bridge", http.StatusSeeOther)
}

// handleBridgeAddNodeManual adds a node by directly supplying individual fields
// for any supported protocol. Credentials are never echoed back in responses.
func (s *Server) handleBridgeAddNodeManual(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	protocol := r.FormValue("protocol")
	tag := strings.TrimSpace(r.FormValue("tag"))
	host := strings.TrimSpace(r.FormValue("host"))
	portStr := r.FormValue("port")
	uuid := r.FormValue("uuid")
	password := r.FormValue("password")
	sni := r.FormValue("sni")
	publicKey := r.FormValue("public_key")
	shortID := r.FormValue("short_id")
	flow := r.FormValue("flow")
	method := r.FormValue("method")
	congestionControl := r.FormValue("congestion_control")

	if tag == "" || host == "" || portStr == "" {
		http.Error(w, "tag, host, and port are required", http.StatusBadRequest)
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return
	}

	var nodeType bridge.NodeType
	switch protocol {
	case "vless-reality":
		nodeType = bridge.NodeTypeVLESSReality
		if uuid == "" || sni == "" || publicKey == "" {
			http.Error(w, "VLESS Reality requires uuid, sni, and public_key", http.StatusBadRequest)
			return
		}
	case "trojan":
		nodeType = bridge.NodeTypeTrojan
		if password == "" || sni == "" {
			http.Error(w, "Trojan requires password and sni", http.StatusBadRequest)
			return
		}
	case "shadowsocks":
		nodeType = bridge.NodeTypeShadowsocks
		if password == "" || method == "" {
			http.Error(w, "Shadowsocks requires password and method", http.StatusBadRequest)
			return
		}
	case "hysteria2":
		nodeType = bridge.NodeTypeHysteria2
		if password == "" || sni == "" {
			http.Error(w, "Hysteria2 requires password and sni", http.StatusBadRequest)
			return
		}
	case "tuic":
		nodeType = bridge.NodeTypeTUIC
		if uuid == "" || password == "" || sni == "" {
			http.Error(w, "TUIC requires uuid, password, and sni", http.StatusBadRequest)
			return
		}
		if congestionControl == "" {
			congestionControl = "bbr"
		}
	default:
		http.Error(w, "unsupported protocol", http.StatusBadRequest)
		return
	}

	node := bridge.Node{
		Type:              nodeType,
		Tag:               tag,
		Host:              host,
		Port:              port,
		UUID:              uuid,
		Password:          password,
		SNI:               sni,
		PublicKey:         publicKey,
		ShortID:           shortID,
		Flow:              flow,
		Method:            method,
		CongestionControl: congestionControl,
		Enabled:           true,
	}

	nodePath := s.nodePath()
	nl, err := bridge.Load(nodePath)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	node.ID = nl.NextID()
	nl.Nodes = append(nl.Nodes, node)
	if err := nl.Save(nodePath); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := s.rerenderSingboxIfActive(nl); err != nil {
		nl.Nodes = nl.Nodes[:len(nl.Nodes)-1]
		_ = nl.Save(nodePath)
		http.Error(w, "node added but sing-box config could not be updated: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Log tag and protocol only — no credential fields.
	audit.Log(s.DB, s.sessionAdminID(r), "node.add", tag, string(nodeType), clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, "../bridge", http.StatusSeeOther)
}

// handleBridgeEditNodeForm renders the edit form for an existing node.
func (s *Server) handleBridgeEditNodeForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}

	nl, err := bridge.Load(s.nodePath())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var found bridge.Node
	var ok bool
	for _, n := range nl.Nodes {
		if n.ID == id {
			found = n
			ok = true
			break
		}
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	csrfToken, _ := NewCSRFToken()
	SetCSRFCookie(w, csrfToken, s.Secure, s.PanelPath)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	editNodePage(w, editNodePageData{
		CSRFField: CSRFField(),
		CSRFToken: csrfToken,
		Node:      found,
		PanelPath: s.PanelPath,
	})
}

// handleBridgeEditNode processes the edit form for a node.
// Only non-credential fields (tag, host, port, sni, flow, method, congestion_control) can be edited.
func (s *Server) handleBridgeEditNode(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}

	tag := strings.TrimSpace(r.FormValue("tag"))
	host := strings.TrimSpace(r.FormValue("host"))
	portStr := r.FormValue("port")
	sni := r.FormValue("sni")
	flow := r.FormValue("flow")
	method := r.FormValue("method")
	congestionControl := r.FormValue("congestion_control")

	if tag == "" {
		http.Error(w, "tag is required", http.StatusBadRequest)
		return
	}
	if host == "" {
		http.Error(w, "host is required", http.StatusBadRequest)
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return
	}

	nodePath := s.nodePath()
	nl, err := bridge.Load(nodePath)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	previous := cloneNodeList(nl)

	var found bool
	for i := range nl.Nodes {
		if nl.Nodes[i].ID == id {
			nl.Nodes[i].Tag = tag
			nl.Nodes[i].Host = host
			nl.Nodes[i].Port = port
			nl.Nodes[i].SNI = sni
			nl.Nodes[i].Flow = flow
			nl.Nodes[i].Method = method
			nl.Nodes[i].CongestionControl = congestionControl
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if err := saveNodeListWithRerenderRollback(s, previous, nl); err != nil {
		http.Error(w, "node updated but sing-box config could not be re-rendered: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Log only safe metadata — no credential fields.
	audit.Log(s.DB, s.sessionAdminID(r), "node.edit", tag, "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, "../../bridge", http.StatusSeeOther)
}

// handleBridgeToggleNode enables or disables a node and re-renders sing-box if active.
func (s *Server) handleBridgeToggleNode(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}

	nodePath := s.nodePath()
	nl, err := bridge.Load(nodePath)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	previous := cloneNodeList(nl)

	var tag string
	found := false
	for i := range nl.Nodes {
		if nl.Nodes[i].ID == id {
			nl.Nodes[i].Enabled = !nl.Nodes[i].Enabled
			tag = nl.Nodes[i].Tag
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if err := saveNodeListWithRerenderRollback(s, previous, nl); err != nil {
		http.Error(w, "node toggled but sing-box config could not be re-rendered: "+err.Error(), http.StatusInternalServerError)
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "node.toggle", tag, "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, "../../bridge", http.StatusSeeOther)
}

// handleBridgeDeleteNode removes a node from the list and re-renders sing-box if active.
func (s *Server) handleBridgeDeleteNode(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}

	nodePath := s.nodePath()
	nl, err := bridge.Load(nodePath)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Guard: prevent deleting the last active node while Bridge mode is enabled.
	if s.singboxIsActive() {
		remaining := 0
		for _, n := range nl.Nodes {
			if n.ID != id && n.Enabled {
				remaining++
			}
		}
		if remaining == 0 {
			http.Error(w, "cannot delete the last active node while Bridge mode is enabled", http.StatusConflict)
			return
		}
	}

	previous := cloneNodeList(nl)

	var tag string
	filtered := nl.Nodes[:0]
	for _, n := range nl.Nodes {
		if n.ID == id {
			tag = n.Tag
			continue // remove this node
		}
		filtered = append(filtered, n)
	}
	nl.Nodes = filtered

	if err := saveNodeListWithRerenderRollback(s, previous, nl); err != nil {
		http.Error(w, "node deleted but sing-box config could not be re-rendered: "+err.Error(), http.StatusInternalServerError)
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "node.delete", tag, "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, "../../bridge", http.StatusSeeOther)
}

// handleBridgePingNode runs a latency test for a single node and redirects back
// with the updated latency. Credentials are never included in error messages.
func (s *Server) handleBridgePingNode(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}

	nodePath := s.nodePath()
	nl, err := bridge.Load(nodePath)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var found bridge.Node
	var foundIdx int = -1
	for i, n := range nl.Nodes {
		if n.ID == id {
			found = n
			foundIdx = i
			break
		}
	}
	if foundIdx < 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Use the local SOCKS5 port exposed by sing-box.
	socksAddr := fmt.Sprintf("%s:%d", singboxSOCKSHost, singboxSOCKSPort)
	target := fmt.Sprintf("%s:%d", found.Host, found.Port)

	latency, dialErr := bridge.DefaultDialer(socksAddr, target)

	now := time.Now()
	nl.Nodes[foundIdx].LastChecked = &now
	if dialErr == nil {
		nl.Nodes[foundIdx].LastLatency = latency.Milliseconds()
	} else {
		nl.Nodes[foundIdx].LastLatency = 0
	}

	if err := nl.Save(nodePath); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Log only the tag — not the error details which might echo host/port.
	audit.Log(s.DB, s.sessionAdminID(r), "node.ping", found.Tag, "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, "../../bridge", http.StatusSeeOther)
}

// handleBridgeSetStrategy saves the routing strategy and re-renders sing-box config
// if Bridge is active. Validates that the selected strategy has enough enabled nodes.
func (s *Server) handleBridgeSetStrategy(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	strategy := r.FormValue("strategy")
	switch singbox.Strategy(strategy) {
	case singbox.StrategyURLTest, singbox.StrategyFallback, singbox.StrategyRoundRobin, singbox.StrategySelector:
		// valid
	default:
		http.Error(w, "invalid strategy", http.StatusBadRequest)
		return
	}

	nodePath := s.nodePath()
	nl, err := bridge.Load(nodePath)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	previous := cloneNodeList(nl)

	activeCount := len(nl.Active())

	// round-robin requires >= 2 enabled nodes (mirrors singbox.Config.Validate()).
	if singbox.Strategy(strategy) == singbox.StrategyRoundRobin && activeCount < 2 {
		http.Error(w, "round-robin strategy requires at least 2 enabled nodes", http.StatusBadRequest)
		return
	}
	if activeCount < 1 {
		http.Error(w, "at least 1 enabled node is required to set a strategy", http.StatusBadRequest)
		return
	}

	nl.Strategy = strategy
	if err := saveNodeListWithRerenderRollback(s, previous, nl); err != nil {
		http.Error(w, "strategy saved but sing-box config could not be updated: "+err.Error(), http.StatusInternalServerError)
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "bridge.strategy", strategy, "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, "../bridge", http.StatusSeeOther)
}

// rerenderSingboxIfActive re-renders and writes sing-box.json when Bridge is active.
// Returns an error when re-render fails — callers should surface this to the admin.
func (s *Server) rerenderSingboxIfActive(nl bridge.NodeList) error {
	exec := realBridgeExecutor{}
	active, _ := exec.ServiceActive("sing-box.service")
	if !active {
		return nil
	}

	svc := &bridge.BridgeService{
		Exec:     exec,
		NodePath: s.nodePath(),
	}
	return svc.RerenderConfig(nl, s.bridgePaths().SingboxJSON)
}

// nodeListToOutbounds converts active bridge nodes to singbox Outbound structs.
func nodeListToOutbounds(nodes []bridge.Node) []singbox.Outbound {
	out := make([]singbox.Outbound, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, singbox.Outbound{
			Type:              singbox.OutboundType(n.Type),
			Tag:               n.Tag,
			Server:            n.Host,
			Port:              n.Port,
			UUID:              n.UUID,
			Flow:              n.Flow,
			TLSServer:         n.SNI,
			PublicKey:         n.PublicKey,
			ShortID:           n.ShortID,
			Password:          n.Password,
			Method:            n.Method,
			CongestionControl: n.CongestionControl,
		})
	}
	return out
}
