package install

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/component/versions"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/nginx"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/secrets"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/singbox"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/stub"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/systemd"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
)

const (
	PanelBackendPort       = 18080
	stubTLSBackendPort     = 9443
	bridgeSOCKS5Addr       = "127.0.0.1:1080"
	bridgeSOCKS5ListenAddr = "127.0.0.1"
	bridgeSOCKS5ListenPort = 1080
)

type StepKind string

const (
	StepAptInstall       StepKind = "apt-install"
	StepCreateDir        StepKind = "create-dir"
	StepDownloadBinary   StepKind = "download-binary"
	StepInstallFile      StepKind = "install-file"
	StepInitPanelDB      StepKind = "init-panel-db"
	StepWriteFile        StepKind = "write-file"
	StepEnsureSystemUser StepKind = "ensure-system-user"
	StepEnableService    StepKind = "enable-service"
	StepStartService     StepKind = "start-service"
	StepReloadService    StepKind = "reload-service"
	StepEnableNginxSite  StepKind = "enable-nginx-site"
	StepDisableNginxSite StepKind = "disable-nginx-site"
)

type LocalBinaries struct {
	CLI   string
	Panel string
}

type PanelBootstrap struct {
	AdminLogin    string
	AdminPassword string
	UserLabel     string
	UserSecretHex string
}

type Step struct {
	Kind        StepKind
	Target      string
	Source      string
	Content     []byte
	Mode        os.FileMode
	URL         string
	SHA256      string
	Bootstrap   *PanelBootstrap
	AptPackages []string
}

// GeneratedCreds holds fresh credentials produced during planning.
type GeneratedCreds struct {
	AdminLogin    string
	AdminPassword string
	PanelPath     string
	FirstUser     secrets.UserSecret
}

// Plan is the complete ordered list of install actions.
type Plan struct {
	Mode  config.Mode
	Steps []Step
	Creds GeneratedCreds
}

// BuildSinglePlan returns the install plan for Single mode.
// It generates fresh credentials and renders all config files but does not touch the host.
func BuildSinglePlan(cfg config.Config, paths config.InstallPaths, panelPort int, binaries LocalBinaries) (Plan, error) {
	creds, err := generateCreds()
	if err != nil {
		return Plan{}, err
	}
	cfg.Mode = config.ModeSingle
	cfg.PanelPath = creds.PanelPath
	return buildPlan(cfg, paths, panelPort, binaries, creds, nil)
}

// BuildBridgePlan returns the install plan for Bridge mode using a first
// VLESS Reality outbound share URL and a local sing-box SOCKS5 listener.
func BuildBridgePlan(cfg config.Config, paths config.InstallPaths, panelPort int, binaries LocalBinaries, firstOutboundURL, strategy string) (Plan, error) {
	if strings.TrimSpace(firstOutboundURL) == "" {
		return Plan{}, fmt.Errorf("build bridge plan: first VLESS Reality outbound URL is required")
	}
	node, err := bridge.ImportVLESS(firstOutboundURL)
	if err != nil {
		return Plan{}, fmt.Errorf("build bridge plan: first outbound must be VLESS Reality: %w", err)
	}
	if strings.TrimSpace(strategy) == "" {
		strategy = cfg.BridgeStrategy
	}
	if strings.TrimSpace(strategy) == "" {
		strategy = "urltest"
	}

	creds, err := generateCreds()
	if err != nil {
		return Plan{}, err
	}
	cfg.Mode = config.ModeBridge
	cfg.BridgeStrategy = strategy
	cfg.PanelPath = creds.PanelPath

	bridgeData, err := bridgePlanData(cfg, paths, node, strategy)
	if err != nil {
		return Plan{}, err
	}
	return buildPlan(cfg, paths, panelPort, binaries, creds, bridgeData)
}

func generateCreds() (GeneratedCreds, error) {
	login, err := secrets.GenerateAdminLogin()
	if err != nil {
		return GeneratedCreds{}, fmt.Errorf("generate admin login: %w", err)
	}
	pass, err := secrets.GenerateAdminPassword()
	if err != nil {
		return GeneratedCreds{}, fmt.Errorf("generate admin password: %w", err)
	}
	panelToken, err := generatePanelToken(login)
	if err != nil {
		return GeneratedCreds{}, fmt.Errorf("generate panel path: %w", err)
	}
	panelPath := "/p-" + panelToken + "/"
	secret, err := secrets.GenerateMTProtoSecret()
	if err != nil {
		return GeneratedCreds{}, fmt.Errorf("generate mtproto secret: %w", err)
	}
	firstUser := secrets.UserSecret{Label: "user1", Secret: secret}

	return GeneratedCreds{
		AdminLogin:    login,
		AdminPassword: pass,
		PanelPath:     panelPath,
		FirstUser:     firstUser,
	}, nil
}

func generatePanelToken(login string) (string, error) {
	for range 8 {
		token, err := secrets.GenerateAdminLogin()
		if err != nil {
			return "", err
		}
		if token != login {
			return token, nil
		}
	}
	return "", fmt.Errorf("panel token matched admin login repeatedly")
}

type bridgeData struct {
	OutboundsJSON []byte
	SingboxJSON   []byte
	SingboxUnit   []byte
}

func buildPlan(cfg config.Config, paths config.InstallPaths, panelPort int, binaries LocalBinaries, creds GeneratedCreds, bridgeMode *bridgeData) (Plan, error) {
	effectiveMaskHost := cfg.MaskHost
	tlsBackend := cfg.TLSBackend
	hasTLSStubBackend := cfg.PanelDomain != "" && cfg.PanelCertPath != "" && cfg.PanelKeyPath != ""
	if hasTLSStubBackend {
		effectiveMaskHost = cfg.PanelDomain
		tlsBackend = fmt.Sprintf("127.0.0.1:%d", stubTLSBackendPort)
		cfg.MaskHost = effectiveMaskHost
		cfg.TLSBackend = tlsBackend
	}
	socks5Addr := ""
	if bridgeMode != nil {
		socks5Addr = bridgeSOCKS5Addr
	}
	tpCfg := teleproxy.Config{
		Port:         cfg.MTProtoPort,
		MaskHost:     effectiveMaskHost,
		TLSBackend:   tlsBackend,
		WildcardMask: cfg.WildcardMask,
		StatsPort:    9091,
		SOCKS5Addr:   socks5Addr,
		MSSClamp:     cfg.MSSClamp,
		JA4Log:       cfg.JA4Log,
		Users: []teleproxy.UserEntry{
			{Label: creds.FirstUser.Label, Secret: creds.FirstUser.Secret.Hex()},
		},
	}
	tpData := tpCfg.Render()

	acmeSnippetPath := ""
	var acmeSnippetData []byte
	if cfg.ACMEEmail != "" && cfg.PanelDomain != "" {
		acmeSnippetPath = paths.NginxSnippetDir + "/acme-challenge.conf"
		acmeSnippetData = nginx.ACMEChallengeConfig{
			WebRootDir: paths.CertDir + "/.well-known-webroot",
		}.Render()
	}

	ngCfg := nginx.StubConfig{
		ListenPort:      80,
		ServerName:      "_",
		StubRoot:        paths.StubDir,
		ACMESnippetPath: acmeSnippetPath,
	}
	ngData := ngCfg.Render()
	var tlsStubData []byte
	if hasTLSStubBackend {
		tlsStubData = nginx.TLSStubConfig{
			ListenPort: stubTLSBackendPort,
			ServerName: cfg.PanelDomain,
			StubRoot:   paths.StubDir,
			CertPath:   cfg.PanelCertPath,
			KeyPath:    cfg.PanelKeyPath,
		}.Render()
	}
	var panelProxyData []byte
	if cfg.PanelDomain != "" && cfg.PanelCertPath != "" && cfg.PanelKeyPath != "" {
		panelProxyData = nginx.PanelProxyConfig{
			ListenPort:  panelPort,
			Domain:      cfg.PanelDomain,
			CertPath:    cfg.PanelCertPath,
			KeyPath:     cfg.PanelKeyPath,
			BackendAddr: fmt.Sprintf("127.0.0.1:%d", PanelBackendPort),
		}.Render()
	}

	tpUnit := systemd.TeleproxyUnitConfig{
		BinaryPath: paths.TeleproxyBin,
		ConfigPath: paths.TeleproxyTOML,
		LogPath:    paths.TeleproxyLog,
	}

	panelUnit := systemd.PanelUnitConfig{
		BinaryPath:    paths.PanelBin,
		ConfigPath:    paths.ConfigFile,
		DBPath:        paths.PanelDB,
		PanelPath:     creds.PanelPath,
		ListenAddr:    fmt.Sprintf("127.0.0.1:%d", PanelBackendPort),
		MTProtoPort:   cfg.MTProtoPort,
		MaskHost:      effectiveMaskHost,
		TLSBackend:    tlsBackend,
		WildcardMask:  cfg.WildcardMask,
		MSSClamp:      cfg.MSSClamp,
		RandomPadding: cfg.RandomPadding,
		JA4Log:        cfg.JA4Log,
		StatsPort:     9091,
		LogPath:       paths.PanelLog,
		ConfigDir:     paths.ConfigDir,
		LogDir:        paths.LogDir,
		BinDir:        paths.BinDir,
		SystemdDir:    paths.SystemdDir,
		StubDir:       paths.StubDir,
		CertDir:       paths.CertDir,
		Domain:        cfg.PanelDomain,
		ACMEEmail:     cfg.ACMEEmail,
	}

	steps := []Step{
		{Kind: StepAptInstall, Target: "nginx"},
		{Kind: StepAptInstall, Target: "curl"},
		{Kind: StepAptInstall, Target: "ca-certificates"},
		{Kind: StepCreateDir, Target: paths.ConfigDir, Mode: 0o700},
		{Kind: StepCreateDir, Target: paths.ConfigDir + "/secrets", Mode: 0o700},
		{Kind: StepCreateDir, Target: paths.ConfigDir + "/nodes", Mode: 0o700},
		{Kind: StepCreateDir, Target: paths.LogDir, Mode: 0o755},
		{Kind: StepCreateDir, Target: paths.StubDir, Mode: 0o755},
		{Kind: StepCreateDir, Target: paths.CertDir, Mode: 0o700},
		{Kind: StepInstallFile, Source: binaries.CLI, Target: paths.CLIBin, Mode: 0o755},
		{Kind: StepInstallFile, Source: binaries.Panel, Target: paths.PanelBin, Mode: 0o755},
		{Kind: StepDownloadBinary, Target: paths.TeleproxyBin, URL: teleproxyDownloadURL(), SHA256: teleproxyDownloadSHA256(), Mode: 0o755},
		{Kind: StepWriteFile, Target: paths.ConfigFile, Content: renderRootConfig(cfg), Mode: 0o600},
		{Kind: StepWriteFile, Target: paths.TeleproxyTOML, Content: tpData, Mode: 0o600},
		{Kind: StepWriteFile, Target: paths.UsersJSON, Content: usersJSONContent(creds.FirstUser), Mode: 0o600},
		{Kind: StepEnsureSystemUser, Target: "teleproxy"},
		{
			Kind:   StepInitPanelDB,
			Target: paths.PanelDB,
			Mode:   0o600,
			Bootstrap: &PanelBootstrap{
				AdminLogin:    creds.AdminLogin,
				AdminPassword: creds.AdminPassword,
				UserLabel:     creds.FirstUser.Label,
				UserSecretHex: creds.FirstUser.Secret.Hex(),
			},
		},
		{Kind: StepWriteFile, Target: paths.TeleproxyService, Content: tpUnit.Render(), Mode: 0o644},
		{Kind: StepWriteFile, Target: "/etc/nginx/sites-available/tgproxy-stub", Content: ngData, Mode: 0o644},
		{Kind: StepDisableNginxSite, Target: "default"},
		{Kind: StepEnableNginxSite, Target: "tgproxy-stub"},
		{Kind: StepReloadService, Target: "nginx"},
		{Kind: StepWriteFile, Target: paths.StubDir + "/index.html", Content: stub.DefaultStubHTML(), Mode: 0o644},
		{Kind: StepWriteFile, Target: paths.PanelService, Content: panelUnit.Render(), Mode: 0o644},
		{Kind: StepEnableService, Target: "teleproxy"},
		{Kind: StepStartService, Target: "teleproxy"},
		{Kind: StepEnableService, Target: "tgproxy-panel"},
		{Kind: StepStartService, Target: "tgproxy-panel"},
	}
	if bridgeMode != nil {
		tpIdx := indexOfWriteTarget(steps, paths.TeleproxyTOML)
		extra := []Step{
			{Kind: StepDownloadBinary, Target: paths.SingboxBin, URL: versions.SingboxLinuxAMD64URL, SHA256: versions.SingboxLinuxAMD64SHA256, Mode: 0o755},
			{Kind: StepWriteFile, Target: paths.OutboundsJSON, Content: bridgeMode.OutboundsJSON, Mode: 0o600},
			{Kind: StepWriteFile, Target: paths.SingboxJSON, Content: bridgeMode.SingboxJSON, Mode: 0o600},
			{Kind: StepWriteFile, Target: paths.SingboxService, Content: bridgeMode.SingboxUnit, Mode: 0o644},
		}
		steps = append(steps[:tpIdx], append(extra, steps[tpIdx:]...)...)
		panelEnableIdx := indexOfServiceStep(steps, StepEnableService, "tgproxy-panel")
		serviceSteps := []Step{
			{Kind: StepEnableService, Target: "sing-box"},
			{Kind: StepStartService, Target: "sing-box"},
		}
		steps = append(steps[:panelEnableIdx], append(serviceSteps, steps[panelEnableIdx:]...)...)
	}
	if len(acmeSnippetData) > 0 {
		// Insert snippet write before the stub site config so the include is valid.
		stubIdx := indexOfWriteTarget(steps, "/etc/nginx/sites-available/tgproxy-stub")
		extra := []Step{
			{Kind: StepCreateDir, Target: paths.NginxSnippetDir, Mode: 0o755},
			{Kind: StepWriteFile, Target: acmeSnippetPath, Content: acmeSnippetData, Mode: 0o644},
		}
		steps = append(steps[:stubIdx], append(extra, steps[stubIdx:]...)...)
	}
	if len(panelProxyData) > 0 {
		insertAt := len(steps) - 5
		extra := []Step{
			{Kind: StepWriteFile, Target: "/etc/nginx/sites-available/tgproxy-panel", Content: panelProxyData, Mode: 0o644},
			{Kind: StepEnableNginxSite, Target: "tgproxy-panel"},
			{Kind: StepReloadService, Target: "nginx"},
		}
		steps = append(steps[:insertAt], append(extra, steps[insertAt:]...)...)
	}
	if len(tlsStubData) > 0 {
		stubIdx := indexOfWriteTarget(steps, paths.StubDir+"/index.html")
		extra := []Step{
			{Kind: StepWriteFile, Target: "/etc/nginx/sites-available/tgproxy-stub-tls", Content: tlsStubData, Mode: 0o644},
			{Kind: StepEnableNginxSite, Target: "tgproxy-stub-tls"},
			{Kind: StepReloadService, Target: "nginx"},
		}
		steps = append(steps[:stubIdx+1], append(extra, steps[stubIdx+1:]...)...)
	}

	return Plan{
		Mode:  cfg.Mode,
		Steps: steps,
		Creds: creds,
	}, nil
}

func bridgePlanData(cfg config.Config, paths config.InstallPaths, node bridge.Node, strategy string) (*bridgeData, error) {
	node.ID = 1
	nodes := bridge.NodeList{
		Nodes:    []bridge.Node{node},
		Strategy: strategy,
	}
	outboundsJSON, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render outbounds.json: %w", err)
	}
	outboundsJSON = append(outboundsJSON, '\n')

	sbCfg := singbox.Config{
		SOCKSListenAddr: bridgeSOCKS5ListenAddr,
		SOCKSListenPort: bridgeSOCKS5ListenPort,
		Strategy:        singbox.Strategy(strategy),
		Outbounds: []singbox.Outbound{{
			Type:      singbox.OutboundVLESSReality,
			Tag:       node.Tag,
			Server:    node.Host,
			Port:      node.Port,
			UUID:      node.UUID,
			Flow:      node.Flow,
			TLSServer: node.SNI,
			PublicKey: node.PublicKey,
			ShortID:   node.ShortID,
		}},
	}
	singboxJSON, err := sbCfg.Render()
	if err != nil {
		return nil, fmt.Errorf("render sing-box.json: %w", err)
	}
	sbUnit := systemd.SingboxUnitConfig{
		BinaryPath: paths.SingboxBin,
		ConfigPath: paths.SingboxJSON,
		LogPath:    paths.SingboxLog,
	}
	_ = cfg
	return &bridgeData{
		OutboundsJSON: outboundsJSON,
		SingboxJSON:   singboxJSON,
		SingboxUnit:   sbUnit.Render(),
	}, nil
}

func teleproxyDownloadURL() string {
	return versions.TeleproxyLinuxAMD64URL
}

func teleproxyDownloadSHA256() string {
	return versions.TeleproxyLinuxAMD64SHA256
}

// indexOfWriteTarget returns the index of the first StepWriteFile step whose
// Target matches target, or len(steps) if not found.
func indexOfWriteTarget(steps []Step, target string) int {
	for i, s := range steps {
		if s.Kind == StepWriteFile && s.Target == target {
			return i
		}
	}
	return len(steps)
}

func indexOfServiceStep(steps []Step, kind StepKind, target string) int {
	for i, s := range steps {
		if s.Kind == kind && s.Target == target {
			return i
		}
	}
	return len(steps)
}

// usersJSONContent returns the initial users.json content with the first user's secret.
func usersJSONContent(firstUser secrets.UserSecret) []byte {
	return []byte(fmt.Sprintf(`{"users":[{"label":%q,"secret":%q}]}`, firstUser.Label, firstUser.Secret.Hex()))
}

func renderRootConfig(cfg config.Config) []byte {
	return []byte(fmt.Sprintf(`mode = %q
mtproto_port = %d
mask_host = %q
tls_backend = %q
wildcard_mask = %q
mss_clamp = %t
random_padding = %t
ja4_log = %t
bridge_strategy = %q
log_level = %q
tcp_keepalive_seconds = %.0f
panel_path = %q
panel_domain = %q
panel_cert_path = %q
panel_key_path = %q
acme_email = %q

# [telegram_bot]
# token = ""
`, cfg.Mode, cfg.MTProtoPort, cfg.MaskHost, cfg.TLSBackend, cfg.WildcardMask, cfg.MSSClamp, cfg.RandomPadding, cfg.JA4Log, cfg.BridgeStrategy, cfg.LogLevel, cfg.TCPKeepalive.Seconds(), cfg.PanelPath, cfg.PanelDomain, cfg.PanelCertPath, cfg.PanelKeyPath, cfg.ACMEEmail))
}
