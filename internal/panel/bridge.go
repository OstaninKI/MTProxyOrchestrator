package panel

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/component"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
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
		SingboxURL:     "", // download handled by installer; panel only re-enables
		SingboxSHA256:  "",
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
	return d.Download(url, sha256hex, destPath)
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
button{padding:.5rem 1rem;background:#2563eb;color:#fff;border:none;border-radius:4px;cursor:pointer}
button:hover{background:#1d4ed8}.danger{background:#dc2626}.danger:hover{background:#b91c1c}</style>
</head>
<body>
<h1>Bridge Mode</h1>
<h2>Add outbound node</h2>
<form method="post" action="bridge/enable">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
<label>VLESS Reality share URL</label>
<input type="text" name="vless_url" placeholder="vless://...#tag" required>
<button type="submit">Enable Bridge</button>
</form>
{{if .Nodes}}
<h2>Current nodes</h2>
<table>
<thead><tr><th>Tag</th><th>Host</th><th>Port</th><th>Enabled</th></tr></thead>
<tbody>
{{range .Nodes}}
<tr>
  <td>{{.Tag}}</td>
  <td>{{.Host}}</td>
  <td>{{.Port}}</td>
  <td>{{if .Enabled}}yes{{else}}no{{end}}</td>
</tr>
{{end}}
</tbody>
</table>
<form method="post" action="bridge/disable" style="margin-top:1rem">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
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
