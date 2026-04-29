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
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
`))

var panelUnitTmpl = template.Must(template.New("panel.service").Parse(`[Unit]
Description=tgproxy admin panel
After=network.target

[Service]
Type=simple
ExecStart={{.BinaryPath}} serve --db {{.DBPath}} --path {{.PanelPath}} --listen {{.ListenAddr}} --mtproto-port {{.MTProtoPort}} --mask-host {{.MaskHost}} --stats-port {{.StatsPort}}
StandardOutput=append:{{.LogPath}}
StandardError=append:{{.LogPath}}
Restart=on-failure
RestartSec=5

NoNewPrivileges=yes
ProtectHome=yes
ProtectSystem=strict
ReadWritePaths={{.ConfigDir}} {{.LogDir}} {{.BinDir}} {{.SystemdDir}}
PrivateTmp=yes

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

[Install]
WantedBy=multi-user.target
`))
