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
		http.Error(w, fmt.Sprintf("invalid vless URL: %v", err), http.StatusBadRequest)
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
		http.Error(w, fmt.Sprintf("bridge enable failed: %v", err), http.StatusInternalServerError)
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
		http.Error(w, fmt.Sprintf("bridge disable failed: %v", err), http.StatusInternalServerError)
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

// --- templates ---

type bridgePageData struct {
	CSRFField string
	CSRFToken string
	Nodes     []bridge.Node
}

var bridgeTmpl = template.Must(template.New("bridge").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Bridge Mode</title>
<style>body{font-family:sans-serif;margin:2rem;color:#333}h1,h2{margin-bottom:1rem}a{color:#2563eb}
table{border-collapse:collapse;width:100%}th,td{text-align:left;padding:.5rem;border-bottom:1px solid #e5e7eb}
input[type=text]{width:100%;box-sizing:border-box;padding:.5rem;border:1px solid #ccc;border-radius:4px;font-size:1rem;margin-bottom:.75rem}
button{padding:.5rem 1rem;background:#2563eb;color:#fff;border:none;border-radius:4px;font-size:.875rem;cursor:pointer}
button:hover{background:#1d4ed8}.danger{background:#dc2626}.danger:hover{background:#b91c1c}
.warn{background:#d97706}.warn:hover{background:#b45309}</style>
</head>
<body>
<h1>Bridge Mode</h1>

<h2>Add outbound node</h2>
<form method="post" action="bridge/nodes/add">
<input type="hidden" name="{{.CSRFField}}" value="{{.CSRFToken}}">
<label>Share URL (vless://, trojan://, ss://, hysteria2://, tuic://)</label>
<input type="text" name="share_url" placeholder="vless://uuid@host:port?...#tag" required>
<button type="submit">Add Node</button>
</form>

{{if .Nodes}}
<h2>Outbound nodes</h2>
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
		http.Error(w, fmt.Sprintf("invalid share URL: %v", err), http.StatusBadRequest)
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

	// Re-render sing-box.json if Bridge is active; errors are best-effort.
	s.rerenderSingboxIfActive(nl)

	// Log tag and type only — never password, uuid, or key material.
	audit.Log(s.DB, s.sessionAdminID(r), "node.add", node.Tag, string(node.Type), clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, "../bridge", http.StatusSeeOther)
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

	if err := nl.Save(nodePath); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.rerenderSingboxIfActive(nl)
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

	if err := nl.Save(nodePath); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.rerenderSingboxIfActive(nl)
	audit.Log(s.DB, s.sessionAdminID(r), "node.delete", tag, "", clientIP(r)) //nolint:errcheck
	http.Redirect(w, r, "../../bridge", http.StatusSeeOther)
}

// rerenderSingboxIfActive re-renders and writes sing-box.json when Bridge is active.
// Errors are best-effort — Bridge mode continues even if re-render fails (config is stale).
func (s *Server) rerenderSingboxIfActive(nl bridge.NodeList) {
	exec := realBridgeExecutor{}
	active, _ := exec.ServiceActive("sing-box.service")
	if !active {
		return
	}

	outbounds := nodeListToOutbounds(nl.Active())
	if len(outbounds) == 0 {
		return
	}

	cfg := singbox.Config{
		SOCKSListenAddr: "127.0.0.1",
		SOCKSListenPort: 1080,
		Strategy:        singbox.StrategyURLTest,
		Outbounds:       outbounds,
	}
	data, err := cfg.Render()
	if err != nil {
		return
	}
	paths := s.bridgePaths()
	_ = exec.WriteFile(paths.SingboxJSON, data, 0o600)
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
