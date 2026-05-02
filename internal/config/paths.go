package config

// InstallPaths holds all filesystem paths used by tgproxy components.
type InstallPaths struct {
	// Directories
	ConfigDir       string
	LogDir          string
	BinDir          string
	SystemdDir      string
	StubDir         string
	CertDir         string // /etc/tgproxy/certs
	NginxSnippetDir string // /etc/nginx/snippets

	// Config files
	ConfigFile    string
	TeleproxyTOML string
	SingboxJSON   string
	UsersJSON     string
	OutboundsJSON string
	PanelDB       string

	// Log files
	PanelLog     string
	TeleproxyLog string
	SingboxLog   string
	NginxLog     string

	// Binaries
	TeleproxyBin string
	SingboxBin   string
	CLIBin       string
	PanelBin     string

	// systemd units
	TeleproxyService string
	SingboxService   string
	PanelService     string
}

// DefaultPaths returns InstallPaths with the spec-defined locations.
func DefaultPaths() InstallPaths {
	return InstallPaths{
		ConfigDir:       "/etc/tgproxy",
		LogDir:          "/var/log/tgproxy",
		BinDir:          "/usr/local/bin",
		SystemdDir:      "/etc/systemd/system",
		StubDir:         "/var/www/tgproxy-stub",
		CertDir:         "/etc/tgproxy/certs",
		NginxSnippetDir: "/etc/nginx/snippets",

		ConfigFile:    "/etc/tgproxy/config.toml",
		TeleproxyTOML: "/etc/tgproxy/teleproxy.toml",
		SingboxJSON:   "/etc/tgproxy/sing-box.json",
		UsersJSON:     "/etc/tgproxy/secrets/users.json",
		OutboundsJSON: "/etc/tgproxy/nodes/outbounds.json",
		PanelDB:       "/etc/tgproxy/panel.db",

		PanelLog:     "/var/log/tgproxy/panel.log",
		TeleproxyLog: "/var/log/tgproxy/teleproxy.log",
		SingboxLog:   "/var/log/tgproxy/sing-box.log",
		NginxLog:     "/var/log/tgproxy/nginx.log",

		TeleproxyBin: "/usr/local/bin/teleproxy",
		SingboxBin:   "/usr/local/bin/sing-box",
		CLIBin:       "/usr/local/bin/tgproxy-cli",
		PanelBin:     "/usr/local/bin/tgproxy-panel",

		TeleproxyService: "/etc/systemd/system/teleproxy.service",
		SingboxService:   "/etc/systemd/system/sing-box.service",
		PanelService:     "/etc/systemd/system/tgproxy-panel.service",
	}
}
