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
	BinaryPath string // /usr/local/bin/tgproxy-panel
	ConfigPath string // /etc/tgproxy/config.toml
	LogPath    string // /var/log/tgproxy/panel.log
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
ExecStart={{.BinaryPath}} --config {{.ConfigPath}}
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
