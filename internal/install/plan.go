package install

import (
	"fmt"
	"os"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/nginx"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/secrets"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/systemd"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
)

const (
	teleproxyLinuxAMD64URL    = "https://github.com/teleproxy/teleproxy/releases/download/v4.12.2/teleproxy-linux-amd64"
	teleproxyLinuxAMD64SHA256 = "02d5e0e4f1f8f44c45eb4c9b3cf6e6bc88c9b4f7f1682622da96eede8f02089f"
)

type StepKind string

const (
	StepAptInstall     StepKind = "apt-install"
	StepCreateDir      StepKind = "create-dir"
	StepDownloadBinary StepKind = "download-binary"
	StepInstallFile    StepKind = "install-file"
	StepInitPanelDB    StepKind = "init-panel-db"
	StepWriteFile      StepKind = "write-file"
	StepEnableService  StepKind = "enable-service"
	StepStartService   StepKind = "start-service"
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
	Kind      StepKind
	Target    string
	Source    string
	Content   []byte
	Mode      os.FileMode
	URL       string
	SHA256    string
	Bootstrap *PanelBootstrap
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
	login, err := secrets.GenerateAdminLogin()
	if err != nil {
		return Plan{}, fmt.Errorf("generate admin login: %w", err)
	}
	pass, err := secrets.GenerateAdminPassword()
	if err != nil {
		return Plan{}, fmt.Errorf("generate admin password: %w", err)
	}
	panelPath := fmt.Sprintf("/p-%s/", login)
	secret, err := secrets.GenerateMTProtoSecret()
	if err != nil {
		return Plan{}, fmt.Errorf("generate mtproto secret: %w", err)
	}
	firstUser := secrets.UserSecret{Label: "user1", Secret: secret}

	creds := GeneratedCreds{
		AdminLogin:    login,
		AdminPassword: pass,
		PanelPath:     panelPath,
		FirstUser:     firstUser,
	}

	tpCfg := teleproxy.Config{
		Port:      cfg.MTProtoPort,
		MaskHost:  cfg.MaskHost,
		StatsPort: 9091,
		Users: []teleproxy.UserEntry{
			{Label: firstUser.Label, Secret: firstUser.Secret.Hex()},
		},
	}
	tpData := tpCfg.Render()

	ngCfg := nginx.StubConfig{
		ListenPort: 80,
		ServerName: "_",
		StubRoot:   paths.StubDir,
	}
	ngData := ngCfg.Render()

	tpUnit := systemd.TeleproxyUnitConfig{
		BinaryPath: paths.TeleproxyBin,
		ConfigPath: paths.TeleproxyTOML,
		LogPath:    paths.TeleproxyLog,
	}

	panelUnit := systemd.PanelUnitConfig{
		BinaryPath:  paths.PanelBin,
		ConfigPath:  paths.ConfigFile,
		DBPath:      paths.PanelDB,
		PanelPath:   panelPath,
		ListenAddr:  fmt.Sprintf("127.0.0.1:%d", panelPort),
		MTProtoPort: cfg.MTProtoPort,
		MaskHost:    cfg.MaskHost,
		StatsPort:   9091,
		LogPath:     paths.PanelLog,
		ConfigDir:   paths.ConfigDir,
		LogDir:      paths.LogDir,
		BinDir:      paths.BinDir,
		SystemdDir:  paths.SystemdDir,
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
		{Kind: StepInstallFile, Source: binaries.CLI, Target: paths.CLIBin, Mode: 0o755},
		{Kind: StepInstallFile, Source: binaries.Panel, Target: paths.PanelBin, Mode: 0o755},
		{Kind: StepDownloadBinary, Target: paths.TeleproxyBin, URL: teleproxyDownloadURL(), SHA256: teleproxyDownloadSHA256()},
		{Kind: StepWriteFile, Target: paths.TeleproxyTOML, Content: tpData, Mode: 0o600},
		{Kind: StepWriteFile, Target: paths.UsersJSON, Content: usersJSONContent(firstUser), Mode: 0o600},
		{
			Kind:   StepInitPanelDB,
			Target: paths.PanelDB,
			Bootstrap: &PanelBootstrap{
				AdminLogin:    creds.AdminLogin,
				AdminPassword: creds.AdminPassword,
				UserLabel:     creds.FirstUser.Label,
				UserSecretHex: creds.FirstUser.Secret.Hex(),
			},
		},
		{Kind: StepWriteFile, Target: paths.TeleproxyService, Content: tpUnit.Render(), Mode: 0o644},
		{Kind: StepWriteFile, Target: "/etc/nginx/sites-available/tgproxy-stub", Content: ngData, Mode: 0o644},
		{Kind: StepWriteFile, Target: paths.StubDir + "/index.html", Content: minimalStubHTML(), Mode: 0o644},
		{Kind: StepWriteFile, Target: paths.PanelService, Content: panelUnit.Render(), Mode: 0o644},
		{Kind: StepEnableService, Target: "teleproxy"},
		{Kind: StepStartService, Target: "teleproxy"},
		{Kind: StepEnableService, Target: "tgproxy-panel"},
		{Kind: StepStartService, Target: "tgproxy-panel"},
	}

	return Plan{
		Mode:  config.ModeSingle,
		Steps: steps,
		Creds: creds,
	}, nil
}

func teleproxyDownloadURL() string {
	return teleproxyLinuxAMD64URL
}

func teleproxyDownloadSHA256() string {
	return teleproxyLinuxAMD64SHA256
}

func minimalStubHTML() []byte {
	return []byte(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>Welcome</title></head>
<body><h1>Welcome</h1></body>
</html>
`)
}

// usersJSONContent returns the initial users.json content with the first user's secret.
func usersJSONContent(firstUser secrets.UserSecret) []byte {
	return []byte(fmt.Sprintf(`{"users":[{"label":%q,"secret":%q}]}`, firstUser.Label, firstUser.Secret.Hex()))
}
