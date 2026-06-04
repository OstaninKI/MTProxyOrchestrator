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

// stubTLSBackendPort is the nginx port that serves real HTTPS for masquerade when
// the panel domain is used as the teleproxy TLS cover domain. Must match the value
// in internal/reconcile and internal/install.
const stubTLSBackendPort = 9443

func systemctlRun(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", args...).Run()
}

// BridgeConfig holds panel-level settings for Bridge mode operations.
// Populated from installation config; injected via Server.BridgeCfg.
type BridgeConfig struct {
	Paths         config.InstallPaths
	MTProtoPort   int
	MaskHost      string
	TLSBackend    string
	WildcardMask  string
	MSSClamp      bool
	RandomPadding bool
	JA4Log        bool
	StatsPort     int
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
		Exec:     s.bridgeExec(),
		NodePath: paths.OutboundsJSON,
	}
	enableCfg := bridge.EnableConfig{
		Node:           node,
		Paths:          paths,
		TeleproxyUsers: entries,
		MTProtoPort:    s.bridgeMTProtoPort(),
		MaskHost:       s.bridgeMaskHost(),
		TLSBackend:     s.bridgeTLSBackend(),
		WildcardMask:   s.bridgeWildcardMask(),
		MSSClamp:       s.bridgeMSSClamp(),
		JA4Log:         s.bridgeJA4Log(),
		StatsPort:      s.bridgeStatsPort(),
		SingboxURL:     singboxDownloadURL(),
		SingboxSHA256:  singboxDownloadSHA256(),
	}
	if err := svc.Enable(enableCfg); err != nil {
		http.Error(w, "bridge enable failed", http.StatusInternalServerError)
		return
	}
	s.DB.SetSetting(settingBridgeMode, string(config.ModeBridge)) //nolint:errcheck

	audit.Log(s.DB, s.sessionAdminID(r), "bridge.enable", node.Tag, "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, s.PanelPath+"bridge", http.StatusSeeOther)
}

// handleBridgeDisable stops sing-box and returns Teleproxy to Single mode.
func (s *Server) handleBridgeDisable(w http.ResponseWriter, r *http.Request) {
	if !ValidateCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}

	if err := s.disableBridgeMode(); err != nil {
		http.Error(w, "bridge disable failed", http.StatusInternalServerError)
		return
	}

	audit.Log(s.DB, s.sessionAdminID(r), "bridge.disable", "", "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, s.PanelPath+"bridge", http.StatusSeeOther)
}

func (s *Server) enableBridgeModeWithNode(node bridge.Node) error {
	users, err := UserRepo{DB: s.DB}.List()
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	var entries []teleproxy.UserEntry
	for _, u := range users {
		if u.Enabled && u.DeletedAt == nil {
			entries = append(entries, teleproxy.UserEntry{Label: u.Label, Secret: u.SecretHex})
		}
	}

	paths := s.bridgePaths()
	svc := &bridge.BridgeService{
		Exec:     s.bridgeExec(),
		NodePath: paths.OutboundsJSON,
	}
	enableCfg := bridge.EnableConfig{
		Node:           node,
		Paths:          paths,
		TeleproxyUsers: entries,
		MTProtoPort:    s.bridgeMTProtoPort(),
		MaskHost:       s.bridgeMaskHost(),
		TLSBackend:     s.bridgeTLSBackend(),
		WildcardMask:   s.bridgeWildcardMask(),
		MSSClamp:       s.bridgeMSSClamp(),
		JA4Log:         s.bridgeJA4Log(),
		StatsPort:      s.bridgeStatsPort(),
		SingboxURL:     singboxDownloadURL(),
		SingboxSHA256:  singboxDownloadSHA256(),
	}
	if s.singboxInstalled() {
		enableCfg.SingboxURL = ""
		enableCfg.SingboxSHA256 = ""
	}
	if err := svc.Enable(enableCfg); err != nil {
		return err
	}
	s.DB.SetSetting(settingBridgeMode, string(config.ModeBridge)) //nolint:errcheck
	return nil
}

func (s *Server) disableBridgeMode() error {
	users, err := UserRepo{DB: s.DB}.List()
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	var entries []teleproxy.UserEntry
	for _, u := range users {
		if u.Enabled && u.DeletedAt == nil {
			entries = append(entries, teleproxy.UserEntry{Label: u.Label, Secret: u.SecretHex})
		}
	}

	paths := s.bridgePaths()
	svc := &bridge.BridgeService{
		Exec:     s.bridgeExec(),
		NodePath: paths.OutboundsJSON,
	}
	disableCfg := bridge.DisableConfig{
		Paths:          paths,
		TeleproxyUsers: entries,
		MTProtoPort:    s.bridgeMTProtoPort(),
		MaskHost:       s.bridgeMaskHost(),
		TLSBackend:     s.bridgeTLSBackend(),
		WildcardMask:   s.bridgeWildcardMask(),
		MSSClamp:       s.bridgeMSSClamp(),
		JA4Log:         s.bridgeJA4Log(),
		StatsPort:      s.bridgeStatsPort(),
	}
	if err := svc.Disable(disableCfg); err != nil {
		return err
	}
	s.DB.SetSetting(settingBridgeMode, string(config.ModeSingle)) //nolint:errcheck
	return nil
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

func (s *Server) bridgeTLSBackend() string {
	if s.BridgeCfg != nil && s.BridgeCfg.TLSBackend != "" {
		return s.BridgeCfg.TLSBackend
	}
	if domain := s.settingsConfig().Domain; domain != "" && s.bridgeMaskHost() == domain {
		return fmt.Sprintf("127.0.0.1:%d", stubTLSBackendPort)
	}
	return ""
}

func (s *Server) bridgeWildcardMask() string {
	if s.BridgeCfg != nil {
		return s.BridgeCfg.WildcardMask
	}
	return ""
}

func (s *Server) bridgeMSSClamp() bool {
	if s.BridgeCfg != nil {
		return s.BridgeCfg.MSSClamp
	}
	return true
}

func (s *Server) bridgeRandomPadding() bool {
	if s.BridgeCfg != nil {
		return s.BridgeCfg.RandomPadding
	}
	return false
}

func (s *Server) bridgeJA4Log() bool {
	if s.BridgeCfg != nil {
		return s.BridgeCfg.JA4Log
	}
	return true
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

// singboxInstalled reports whether the sing-box binary is present on disk.
// Uses the injected SingboxInstalled func if set (for tests), otherwise stats
// the configured binary path.
func (s *Server) singboxInstalled() bool {
	if s.SingboxInstalled != nil {
		return s.SingboxInstalled()
	}
	info, err := os.Stat(s.bridgePaths().SingboxBin)
	return err == nil && !info.IsDir()
}

// ensureSingboxInstalled downloads and SHA256-verifies the sing-box binary when
// it is not already present. This lets an operator add a Bridge node even when
// the system was installed in Single mode (sing-box is only installed on demand).
// It is a no-op when the binary already exists. Enabling and starting the
// sing-box service remains the responsibility of the Bridge enable flow.
func (s *Server) ensureSingboxInstalled() error {
	if s.singboxInstalled() {
		return nil
	}
	if err := s.bridgeExec().Download(singboxDownloadURL(), singboxDownloadSHA256(), s.bridgePaths().SingboxBin); err != nil {
		return fmt.Errorf("download sing-box: %w", err)
	}
	return nil
}

// bridgeExec returns the bridge executor to use for OS operations.
// Falls back to realBridgeExecutor{} when Server.BridgeExec is nil (production).
func (s *Server) bridgeExec() bridge.Executor {
	if s.BridgeExec != nil {
		return s.BridgeExec
	}
	return realBridgeExecutor{}
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
    <p class="page-eyebrow">MTProto Orchestrator</p>
    <h1 class="page-title">Bridge</h1>
    <p class="page-sub">Outbound nodes and routing strategy.</p>
  </div>
  <div class="actions">
    <button class="btn" data-variant="primary" type="button" data-bridge-open-add>{{icon "Plus" 13}} Add node</button>
  </div>
</section>
<section class="page-stack" data-bridge-page>
{{if .Flash}}<div class="flash">{{.Flash}}</div>{{end}}
<section class="card bridge-banner">
  <div class="card-body">
    <div class="row row-center-wide bridge-banner-main">
      <div class="ring-lite mono">{{countEnabledBridgeNodes .Nodes}}/{{len .Nodes}}</div>
      <div class="col col-tight col-fill">
        <div class="row row-tight">
          <span class="setting-title">Bridge mode</span>
          <span class="badge" data-tone="accent"><span class="dot"></span>{{if .Strategy}}{{.Strategy}}{{else}}urltest{{end}} strategy</span>
        </div>
        <span class="muted-md">Route outbound proxy traffic through VLESS Reality, Trojan, Shadowsocks, Hysteria2, or TUIC. Active connections use the configured strategy across enabled nodes.</span>
      </div>
      <div class="col col-tight text-right">
        <span class="mono value-lg">{{if gt (avgBridgeLatency .Nodes) 0}}{{avgBridgeLatency .Nodes}}ms{{else}}—{{end}}</span>
        <span class="muted-sm">avg latency</span>
      </div>
    </div>
  </div>
  <nav class="seg bridge-seg" aria-label="Bridge sections">
    <a class="seg-item active" href="#nodes">Nodes ({{len .Nodes}})</a>
    <a class="seg-item" href="#add-node">Add node</a>
    <a class="seg-item" href="#routing-strategy">Routing</a>
    <a class="seg-item" href="#mode-control">Mode control</a>
  </nav>
</section>

{{if .Nodes}}
<div id="nodes" class="card table-card">
<div class="card-head">
<div class="col card-title-stack"><h3>Outbound Nodes</h3><span class="sub">Status reflects whether the node is enabled in the Bridge config.</span></div>
</div>
<div class="card-body card-body--flush"><table class="tbl">
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
          <span class="meter-fill pct-{{bridgeLatencyPct .LastLatency}}"></span>
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
      <button class="btn" data-size="xs" type="submit">{{if .Enabled}}Disable{{else}}Enable{{end}}</button>
    </form>
    <form method="post" action="{{$.PanelPath}}bridge/nodes/{{.ID}}/ping" class="inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button class="btn" data-size="xs" data-variant="ghost" type="submit">Test latency</button>
    </form>
    <a class="btn" data-size="xs" data-variant="ghost" href="{{$.PanelPath}}bridge/nodes/{{.ID}}/edit">Edit</a>
    <form method="post" action="{{$.PanelPath}}bridge/nodes/{{.ID}}/delete" class="inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button class="btn danger" data-size="xs" type="submit">Delete</button>
    </form>
    </div>
  </td>
</tr>
{{end}}
</tbody>
</table></div>
</div>

<div id="routing-strategy" class="card">
<div class="card-head"><h3>Outbound strategy</h3><span class="sub">How traffic is distributed across online nodes</span></div>
<div class="card-body">
<form method="post" action="{{.PanelPath}}bridge/strategy" class="bridge-form">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<select name="strategy">
  <option value="urltest"{{if eq .Strategy "urltest"}} selected{{end}}{{if eq .Strategy ""}} selected{{end}}>urltest (auto — lowest latency)</option>
  <option value="fallback"{{if eq .Strategy "fallback"}} selected{{end}}>fallback (primary + fallback)</option>
  <option value="roundrobin"{{if eq .Strategy "roundrobin"}} selected{{end}}>round-robin (rotate through nodes)</option>
  <option value="selector"{{if eq .Strategy "selector"}} selected{{end}}>selector (manual)</option>
</select>
<button class="btn" data-variant="primary" type="submit">{{icon "Check" 12}} Save Strategy</button>
</form>
</div>
</div>

<div id="mode-control" class="grid-12">
  <div class="col-7">
    <section class="card">
      <div class="card-head"><div class="col card-title-stack"><h3>Mode control</h3><span class="sub">Switch between Single and Bridge mode.</span></div></div>
      <div class="card-body">
        <form method="post" action="{{.PanelPath}}bridge/enable" class="bridge-form">
          <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
          <label class="label">Enable Bridge with share URL</label>
          <input class="input input--mono" type="text" name="vless_url" placeholder="vless://...#tag (VLESS Reality only for first enable)">
          <span class="help">Bootstraps Bridge mode from a single VLESS Reality share URL.</span>
          <button class="btn" data-variant="primary" type="submit">{{icon "Check" 12}} Enable Bridge</button>
        </form>
      </div>
    </section>
  </div>
  <div class="col-5">
    <section class="card">
      <div class="card-head"><h3>Return to Single</h3></div>
      <div class="card-body">
        <form method="post" action="{{.PanelPath}}bridge/disable" class="bridge-form">
          <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
          <p class="panel-note">Stops sing-box and rewrites Teleproxy back to Single mode.</p>
          <button class="btn danger" type="submit">Disable Bridge</button>
        </form>
      </div>
    </section>
  </div>
</div>
{{end}}

<div class="modal-scrim" data-bridge-modal hidden>
  <div class="modal" role="dialog" aria-modal="true" aria-labelledby="bridge-add-title">
    <div class="modal-head">
      <h2 id="bridge-add-title">Add outbound node</h2>
      <p>Import from a share URL or enter the routing fields manually.</p>
    </div>
    <div class="modal-body">
      <section class="detail-section">
        <h3 class="detail-section-title">From share URL</h3>
        <form method="post" action="{{.PanelPath}}bridge/nodes/add" class="bridge-form">
          <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}" class="js-csrf">
          <label class="label">Share URL</label>
          <input class="input input--mono" type="text" name="share_url" placeholder="vless://uuid@host:port?...#tag" required>
          <span class="help">Supported: vless://, trojan://, ss://, hysteria2://, tuic://.</span>
          <div class="row row-end row-tight"><button class="btn" data-variant="primary" type="submit">{{icon "Plus" 13}} Add node</button></div>
        </form>
      </section>
      <details class="card disclosure">
        <summary>Add node manually</summary>
        <form method="post" action="{{.PanelPath}}bridge/nodes/add-manual" class="bridge-form">
          <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}" class="js-csrf">
          <label class="label">Protocol</label>
          <select class="select" name="protocol" required>
            <option value="vless-reality">VLESS Reality</option>
            <option value="trojan">Trojan</option>
            <option value="shadowsocks">Shadowsocks</option>
            <option value="hysteria2">Hysteria2</option>
            <option value="tuic">TUIC</option>
          </select>
          <label class="label">Tag (name)</label>
          <input class="input" type="text" name="tag" placeholder="my-node" required>
          <label class="label">Host</label>
          <input class="input" type="text" name="host" placeholder="1.2.3.4 or hostname" required>
          <label class="label">Port</label>
          <input class="input input--mono" type="number" name="port" placeholder="443" min="1" max="65535" required>
          <label class="label">UUID (VLESS, TUIC)</label>
          <input class="input input--mono" type="text" name="uuid" placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx">
          <label class="label">Password (Trojan, SS, Hysteria2, TUIC)</label>
          <input class="input input--mono" type="text" name="password" placeholder="password">
          <label class="label">SNI (VLESS Reality, Trojan, Hysteria2, TUIC)</label>
          <input class="input" type="text" name="sni" placeholder="example.com">
          <label class="label">Public Key (VLESS Reality)</label>
          <input class="input input--mono" type="text" name="public_key" placeholder="base64 public key">
          <label class="label">Short ID (VLESS Reality, may be empty)</label>
          <input class="input input--mono" type="text" name="short_id" placeholder="">
          <label class="label">Flow (VLESS Reality, optional)</label>
          <input class="input input--mono" type="text" name="flow" placeholder="xtls-rprx-vision">
          <label class="label">Method/Cipher (Shadowsocks)</label>
          <input class="input input--mono" type="text" name="method" placeholder="chacha20-ietf-poly1305">
          <label class="label">Congestion Control (TUIC, default: bbr)</label>
          <input class="input input--mono" type="text" name="congestion_control" placeholder="bbr">
          <div class="row row-end row-tight"><button class="btn" data-variant="primary" type="submit">{{icon "Plus" 13}} Add manually</button></div>
        </form>
      </details>
    </div>
    <div class="modal-foot">
      <button class="btn" data-variant="ghost" type="button" data-bridge-close-add>Close</button>
    </div>
  </div>
</div>
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
  <div class="actions"><a class="btn" data-variant="ghost" href="{{.PanelPath}}bridge">Back to bridge</a></div>
</section>
<section class="grid-12">
  <div class="col-7">
    <div class="card">
      {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
      <div class="card-head"><div class="col card-title-stack"><h3>Visible routing fields</h3><span class="sub">Credentials stay write-only.</span></div></div>
      <div class="card-body">
        <form method="post" action="" class="stack-form">
          <input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
          <label class="label">Tag (name)</label>
          <input class="input" type="text" name="tag" value="{{.Node.Tag}}" required>
          <label class="label">Host</label>
          <input class="input" type="text" name="host" value="{{.Node.Host}}" required>
          <label class="label">Port</label>
          <input class="input input--mono" type="number" name="port" value="{{.Node.Port}}" min="1" max="65535" required>
          <label class="label">SNI</label>
          <input class="input" type="text" name="sni" value="{{.Node.SNI}}">
          <label class="label">Flow (VLESS Reality)</label>
          <input class="input input--mono" type="text" name="flow" value="{{.Node.Flow}}">
          <label class="label">Method/Cipher (Shadowsocks)</label>
          <input class="input input--mono" type="text" name="method" value="{{.Node.Method}}">
          <label class="label">Congestion Control (TUIC)</label>
          <input class="input input--mono" type="text" name="congestion_control" value="{{.Node.CongestionControl}}">
          <div class="row row-end row-tight">
            <a class="btn" data-variant="ghost" href="{{.PanelPath}}bridge">Cancel</a>
            <button class="btn" data-variant="primary" type="submit">{{icon "Check" 12}} Save</button>
          </div>
        </form>
      </div>
    </div>
  </div>
  <div class="col-5">
    <div class="card">
      <div class="card-head"><h3>Notes</h3></div>
      <div class="card-body col col-panel">
        <div class="totp-note-row"><span class="badge warn">Credentials</span><span class="col totp-note-copy"><strong class="totp-note-title">Write-only</strong><span class="help">UUID, password, public key, and short ID are intentionally hidden here.</span></span></div>
        <div class="totp-note-row"><span class="badge">Rotation</span><span class="col totp-note-copy"><strong class="totp-note-title">Re-import to replace secrets</strong><span class="help">Delete and add the node again if credential material changed.</span></span></div>
      </div>
    </div>
  </div>
</section>
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

	if !s.singboxIsActive() {
		if err := s.enableBridgeModeWithNode(node); err != nil {
			http.Error(w, "bridge enable failed", http.StatusInternalServerError)
			return
		}
		audit.Log(s.DB, s.sessionAdminID(r), "bridge.enable", node.Tag, "", clientIP(r))           //nolint:errcheck
		audit.Log(s.DB, s.sessionAdminID(r), "node.add", node.Tag, string(node.Type), clientIP(r)) //nolint:errcheck
		http.Redirect(w, r, s.PanelPath+"bridge", http.StatusSeeOther)
		return
	}

	// Install sing-box on demand if the system was set up in Single mode.
	if err := s.ensureSingboxInstalled(); err != nil {
		http.Error(w, "could not install sing-box: "+err.Error(), http.StatusInternalServerError)
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
	http.Redirect(w, r, s.PanelPath+"bridge", http.StatusSeeOther)
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
	fingerprint := r.FormValue("fingerprint")

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
		if fingerprint == "" {
			fingerprint = "chrome"
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
		Fingerprint:       fingerprint,
		Method:            method,
		CongestionControl: congestionControl,
		Enabled:           true,
	}

	if !s.singboxIsActive() {
		if err := s.enableBridgeModeWithNode(node); err != nil {
			http.Error(w, "bridge enable failed", http.StatusInternalServerError)
			return
		}
		audit.Log(s.DB, s.sessionAdminID(r), "bridge.enable", tag, "", clientIP(r))          //nolint:errcheck
		audit.Log(s.DB, s.sessionAdminID(r), "node.add", tag, string(nodeType), clientIP(r)) //nolint:errcheck
		http.Redirect(w, r, s.PanelPath+"bridge", http.StatusSeeOther)
		return
	}

	// Install sing-box on demand if the system was set up in Single mode.
	if err := s.ensureSingboxInstalled(); err != nil {
		http.Error(w, "could not install sing-box: "+err.Error(), http.StatusInternalServerError)
		return
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
	http.Redirect(w, r, s.PanelPath+"bridge", http.StatusSeeOther)
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
	fingerprint := r.FormValue("fingerprint")

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
			if nl.Nodes[i].Type == bridge.NodeTypeVLESSReality {
				if fingerprint == "" {
					fingerprint = "chrome"
				}
				nl.Nodes[i].Fingerprint = fingerprint
			}
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
	http.Redirect(w, r, s.PanelPath+"bridge", http.StatusSeeOther)
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

	if s.singboxIsActive() && len(nl.Active()) == 0 {
		if err := nl.Save(nodePath); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := s.disableBridgeMode(); err != nil {
			if restoreErr := previous.Save(nodePath); restoreErr != nil {
				http.Error(w, "node toggled but Bridge could not switch to Single mode: "+err.Error()+" (rollback failed: "+restoreErr.Error()+")", http.StatusInternalServerError)
				return
			}
			http.Error(w, "node toggled but Bridge could not switch to Single mode: "+err.Error(), http.StatusInternalServerError)
			return
		}
		audit.Log(s.DB, s.sessionAdminID(r), "bridge.disable", "last-node-disabled", "", clientIP(r)) //nolint:errcheck
	} else {
		if err := saveNodeListWithRerenderRollback(s, previous, nl); err != nil {
			http.Error(w, "node toggled but sing-box config could not be re-rendered: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	audit.Log(s.DB, s.sessionAdminID(r), "node.toggle", tag, "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, s.PanelPath+"bridge", http.StatusSeeOther)
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

	previous := cloneNodeList(nl)

	var tag string
	found := false
	filtered := nl.Nodes[:0]
	for _, n := range nl.Nodes {
		if n.ID == id {
			tag = n.Tag
			found = true
			continue // remove this node
		}
		filtered = append(filtered, n)
	}
	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	nl.Nodes = filtered

	if s.singboxIsActive() && len(nl.Active()) == 0 {
		if err := nl.Save(nodePath); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := s.disableBridgeMode(); err != nil {
			if restoreErr := previous.Save(nodePath); restoreErr != nil {
				http.Error(w, "node deleted but Bridge could not switch to Single mode: "+err.Error()+" (rollback failed: "+restoreErr.Error()+")", http.StatusInternalServerError)
				return
			}
			http.Error(w, "node deleted but Bridge could not switch to Single mode: "+err.Error(), http.StatusInternalServerError)
			return
		}
		audit.Log(s.DB, s.sessionAdminID(r), "bridge.disable", "last-node-deleted", "", clientIP(r)) //nolint:errcheck
	} else {
		if err := saveNodeListWithRerenderRollback(s, previous, nl); err != nil {
			http.Error(w, "node deleted but sing-box config could not be re-rendered: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	audit.Log(s.DB, s.sessionAdminID(r), "node.delete", tag, "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, s.PanelPath+"bridge", http.StatusSeeOther)
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
	http.Redirect(w, r, s.PanelPath+"bridge", http.StatusSeeOther)
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
	http.Redirect(w, r, s.PanelPath+"bridge", http.StatusSeeOther)
}

// rerenderSingboxIfActive re-renders and writes sing-box.json when Bridge is active.
// Returns an error when re-render fails — callers should surface this to the admin.
func (s *Server) rerenderSingboxIfActive(nl bridge.NodeList) error {
	exec := s.bridgeExec()
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
			Fingerprint:       n.Fingerprint,
			Password:          n.Password,
			Method:            n.Method,
			CongestionControl: n.CongestionControl,
		})
	}
	return out
}
