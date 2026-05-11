package systemd

import (
	"bytes"
	"fmt"
	"text/template"
)

// TeleproxyUnitConfig holds fields for teleproxy.service.
type TeleproxyUnitConfig struct {
	BinaryPath string // /usr/local/bin/teleproxy
	ConfigPath string // /etc/tgproxy/teleproxy.toml
	LogPath    string // /var/log/tgproxy/teleproxy.log
}

// PanelUnitConfig holds fields for tgproxy-panel.service.
type PanelUnitConfig struct {
	BinaryPath  string // /usr/local/bin/tgproxy-panel
	ConfigPath  string // /etc/tgproxy/config.toml
	DBPath      string // /etc/tgproxy/panel.db
	PanelPath   string // /p-random/
	ListenAddr  string // 127.0.0.1:8443
	MTProtoPort int    // 443
	MaskHost    string // www.microsoft.com
	StatsPort   int    // 9091
	LogPath     string // /var/log/tgproxy/panel.log
	ConfigDir   string // /etc/tgproxy
	LogDir      string // /var/log/tgproxy
	BinDir      string // /usr/local/bin
	SystemdDir  string // /etc/systemd/system
	StubDir     string // /var/www/tgproxy-stub
	// Optional ACME fields — when non-empty the panel starts the renewal loop.
	CertDir   string // /etc/tgproxy/certs
	Domain    string // proxy.example.com
	ACMEEmail string // admin@example.com
}

func (c TeleproxyUnitConfig) Render() []byte {
	var buf bytes.Buffer
	if err := teleproxyUnitTmpl.Execute(&buf, c); err != nil {
		panic(fmt.Sprintf("teleproxy unit render: %v", err))
	}
	return buf.Bytes()
}

func (c PanelUnitConfig) Render() []byte {
	var buf bytes.Buffer
	if err := panelUnitTmpl.Execute(&buf, c); err != nil {
		panic(fmt.Sprintf("panel unit render: %v", err))
	}
	return buf.Bytes()
}

var teleproxyUnitTmpl = template.Must(template.New("teleproxy.service").Parse(`[Unit]
Description=Teleproxy MTProto proxy
After=network.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} --config {{.ConfigPath}}
StandardOutput=append:{{.LogPath}}
StandardError=append:{{.LogPath}}
Restart=on-failure
RestartSec=5

NoNewPrivileges=yes
ProtectHome=yes
ProtectSystem=strict
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
LockPersonality=yes
RestrictRealtime=yes
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_SETUID CAP_SETGID
CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_SETUID CAP_SETGID

[Install]
WantedBy=multi-user.target
`))

// The panel mutates configuration and SQLite state under /etc/tgproxy, which
// this project keeps as root-owned 0600 files. DynamicUser is incompatible with
// that storage model unless the on-disk ownership model changes.
var panelUnitTmpl = template.Must(template.New("panel.service").Parse(`[Unit]
Description=tgproxy admin panel
After=network.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} serve --db {{.DBPath}} --path {{.PanelPath}} --listen {{.ListenAddr}} --mtproto-port {{.MTProtoPort}} --mask-host {{.MaskHost}} --stats-port {{.StatsPort}}{{if .StubDir}} --stub-dir {{.StubDir}}{{end}}{{if .CertDir}} --cert-dir {{.CertDir}}{{end}}{{if .Domain}} --domain {{.Domain}}{{end}}{{if .ACMEEmail}} --acme-email {{.ACMEEmail}}{{end}}
StandardOutput=append:{{.LogPath}}
StandardError=append:{{.LogPath}}
Restart=on-failure
RestartSec=5

NoNewPrivileges=yes
UMask=0077
ProtectHome=yes
ProtectSystem=strict
ReadWritePaths={{.ConfigDir}} {{.LogDir}} {{.BinDir}} {{.SystemdDir}}{{if .StubDir}} {{.StubDir}}{{end}}{{if .CertDir}} {{.CertDir}}{{end}}
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
LockPersonality=yes
RestrictRealtime=yes
CapabilityBoundingSet=

[Install]
WantedBy=multi-user.target
`))

// SingboxUnitConfig holds fields for sing-box.service.
type SingboxUnitConfig struct {
	BinaryPath string // /usr/local/bin/sing-box
	ConfigPath string // /etc/tgproxy/sing-box.json
	LogPath    string // /var/log/tgproxy/sing-box.log
}

func (c SingboxUnitConfig) Render() []byte {
	var buf bytes.Buffer
	if err := singboxUnitTmpl.Execute(&buf, c); err != nil {
		panic(fmt.Sprintf("sing-box unit render: %v", err))
	}
	return buf.Bytes()
}

var singboxUnitTmpl = template.Must(template.New("sing-box.service").Parse(`[Unit]
Description=sing-box outbound router
After=network.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} run --config {{.ConfigPath}}
StandardOutput=append:{{.LogPath}}
StandardError=append:{{.LogPath}}
Restart=on-failure
RestartSec=5

NoNewPrivileges=yes
ProtectHome=yes
ProtectSystem=strict
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
LockPersonality=yes
RestrictRealtime=yes

[Install]
WantedBy=multi-user.target
`))
