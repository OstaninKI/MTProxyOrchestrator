package panel

import (
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
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/singbox"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
)

const (
	singboxLinuxAMD64URL    = "https://github.com/SagerNet/sing-box/releases/download/v1.13.11/sing-box-1.13.11-linux-amd64.tar.gz"
	singboxLinuxAMD64SHA256 = "10ff037632165ca4f6472a0ec21393280ef5a33677e05bcde7fbcf6f9737637b"
)

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
	SetCSRFCookie(w, csrfToken, s.Secure)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	bridgePage(w, bridgePageData{
		CSRFField: CSRFField(),
		CSRFToken: csrfToken,
		Nodes:     nl.Nodes,
		Strategy:  nl.Strategy,
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
	if s.BridgeCfg != nil {
		return s.BridgeCfg.Paths
	}
	return config.DefaultPaths()
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
	return singboxLinuxAMD64URL
}

func singboxDownloadSHA256() string {
	return singboxLinuxAMD64SHA256
}

func (realBridgeExecutor) EnableService(name string) error {
	return exec.Command("systemctl", "enable", name).Run()
}

func (realBridgeExecutor) StartService(name string) error {
	return exec.Command("systemctl", "restart", name).Run()
}

func (realBridgeExecutor) StopService(name string) error {
	return exec.Command("systemctl", "stop", name).Run()
}

func (realBridgeExecutor) DisableService(name string) error {
	return exec.Command("systemctl", "disable", name).Run()
}

func (realBridgeExecutor) ServiceActive(name string) (bool, error) {
	err := exec.Command("systemctl", "is-active", name).Run()
	return err == nil, nil
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
	CSRFField string
	CSRFToken string
	Nodes     []bridge.Node
	Strategy  string
	Flash     string // optional informational message
}

var bridgeTmpl = template.Must(template.New("bridge").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Bridge Mode</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1,h2{margin-bottom:1rem}a{color:#2563eb}
table{border-collapse:collapse;width:100%}th,td{text-align:left;padding:.5rem;border-bottom:1px solid #e5e7eb}
input[type=text],input[type=number],select{width:100%;box-sizing:border-box;padding:.5rem;border:1px solid #ccc;border-radius:4px;font-size:1rem;margin-bottom:.75rem}
button{padding:.5rem 1rem;background:#2563eb;color:#fff;border:none;border-radius:4px;font-size:.875rem;cursor:pointer}
button:hover{background:#1d4ed8}.danger{background:#dc2626}.danger:hover{background:#b91c1c}
.warn{background:#d97706}.warn:hover{background:#b45309}
.success{color:#16a34a;background:#f0fdf4;border:1px solid #bbf7d0;padding:.75rem;border-radius:4px;margin-bottom:1rem}
.flash{color:#1d4ed8;background:#eff6ff;border:1px solid #bfdbfe;padding:.75rem;border-radius:4px;margin-bottom:1rem}
</style>
</head>
<body>
<h1>Bridge Mode</h1>

{{if .Flash}}<div class="flash">{{.Flash}}</div>{{end}}

<h2>Add outbound node via share URL</h2>
<form method="post" action="bridge/nodes/add">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Share URL (vless://, trojan://, ss://, hysteria2://, tuic://)</label>
<input type="text" name="share_url" placeholder="vless://uuid@host:port?...#tag" required>
<button type="submit">Add Node</button>
</form>

<h2 style="margin-top:1.5rem">Add node manually</h2>
<details>
<summary style="cursor:pointer;color:#2563eb">Expand manual add form</summary>
<form method="post" action="bridge/nodes/add-manual" style="margin-top:1rem">
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
<h2 style="margin-top:2rem">Outbound nodes</h2>
<table>
<thead><tr><th>Tag</th><th>Type</th><th>Host</th><th>Port</th><th>Status</th><th>Latency</th><th>Actions</th></tr></thead>
<tbody>
{{range .Nodes}}
<tr>
  <td>{{.Tag}}</td>
  <td>{{.Type}}</td>
  <td>{{.Host}}</td>
  <td>{{.Port}}</td>
  <td>{{if .Enabled}}<span style="color:#16a34a">enabled</span>{{else}}<span style="color:#6b7280">disabled</span>{{end}}</td>
  <td>{{if .LastLatency}}{{.LastLatency}}ms{{else}}—{{end}}</td>
  <td>
    <form method="post" action="bridge/nodes/{{.ID}}/toggle" style="display:inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit" class="warn">{{if .Enabled}}Disable{{else}}Enable{{end}}</button>
    </form>
    <form method="post" action="bridge/nodes/{{.ID}}/ping" style="display:inline">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit">Test Latency</button>
    </form>
    <a href="bridge/nodes/{{.ID}}/edit"><button type="button">Edit</button></a>
    <form method="post" action="bridge/nodes/{{.ID}}/delete" style="display:inline"
          onsubmit="return confirm('Delete node {{.Tag}}?')">
      <input type="hidden" name="{{$.CSRFField}}" value="{{$.CSRFToken}}">
      <button type="submit" class="danger">Delete</button>
    </form>
  </td>
</tr>
{{end}}
</tbody>
</table>

<h2 style="margin-top:2rem">Routing strategy</h2>
<form method="post" action="bridge/strategy">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<select name="strategy">
  <option value="urltest"{{if eq .Strategy "urltest"}} selected{{end}}{{if eq .Strategy ""}} selected{{end}}>urltest (auto — lowest latency)</option>
  <option value="fallback"{{if eq .Strategy "fallback"}} selected{{end}}>fallback (primary + fallback)</option>
  <option value="roundrobin"{{if eq .Strategy "roundrobin"}} selected{{end}}>round-robin (rotate through nodes)</option>
  <option value="selector"{{if eq .Strategy "selector"}} selected{{end}}>selector (manual)</option>
</select>
<button type="submit">Save Strategy</button>
</form>

<h2 style="margin-top:2rem">Mode control</h2>
<form method="post" action="bridge/enable">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Enable Bridge with share URL</label>
<input type="text" name="vless_url" placeholder="vless://...#tag (VLESS Reality only for first enable)">
<button type="submit">Enable Bridge</button>
</form>
<form method="post" action="bridge/disable" style="margin-top:1rem">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<button type="submit" class="danger">Disable Bridge (return to Single)</button>
</form>
{{end}}

<p style="margin-top:2rem"><a href="../dashboard">← Dashboard</a></p>
</body>
</html>
`))

func bridgePage(w io.Writer, data bridgePageData) {
	bridgeTmpl.Execute(w, data) //nolint:errcheck
}

// editNodeTmpl is the template for the node edit form.
var editNodeTmpl = template.Must(template.New("editNode").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Edit Node</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1{margin-bottom:1rem}
input[type=text],input[type=number]{width:100%;box-sizing:border-box;padding:.5rem;border:1px solid #ccc;border-radius:4px;font-size:1rem;margin-bottom:.75rem}
button{padding:.5rem 1rem;background:#2563eb;color:#fff;border:none;border-radius:4px;font-size:.875rem;cursor:pointer}
button:hover{background:#1d4ed8}.flash{color:#dc2626;margin-bottom:1rem}</style>
</head>
<body>
<h1>Edit Node: {{.Node.Tag}}</h1>
{{if .Error}}<p class="flash">{{.Error}}</p>{{end}}
<form method="post" action="">
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
<p style="color:#6b7280;font-size:.875rem">Credentials (UUID, password, public key, short ID) are not shown and cannot be changed here. Delete and re-add the node to change credentials.</p>
<button type="submit">Save</button>
</form>
<p><a href="../../bridge">← Back</a></p>
</body>
</html>
`))

type editNodePageData struct {
	CSRFField string
	CSRFToken string
	Node      bridge.Node
	Error     string
}

func editNodePage(w io.Writer, data editNodePageData) {
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
	SetCSRFCookie(w, csrfToken, s.Secure)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	editNodePage(w, editNodePageData{
		CSRFField: CSRFField(),
		CSRFToken: csrfToken,
		Node:      found,
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
