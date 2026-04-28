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

type StepKind string

const (
	StepAptInstall     StepKind = "apt-install"
	StepCreateDir      StepKind = "create-dir"
	StepDownloadBinary StepKind = "download-binary"
	StepWriteFile      StepKind = "write-file"
	StepEnableService  StepKind = "enable-service"
	StepStartService   StepKind = "start-service"
)

type Step struct {
	Kind    StepKind
	Target  string
	Content []byte
	Mode    os.FileMode
	URL     string
	SHA256  string
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
func BuildSinglePlan(cfg config.Config, paths config.InstallPaths, panelPort int) (Plan, error) {
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
		BinaryPath: paths.PanelBin,
		ConfigPath: paths.ConfigFile,
		LogPath:    paths.PanelLog,
	}

	steps := []Step{
		{Kind: StepAptInstall, Target: "nginx"},
		{Kind: StepAptInstall, Target: "curl"},
		{Kind: StepAptInstall, Target: "ca-certificates"},
		{Kind: StepCreateDir, Target: paths.ConfigDir, Mode: 0o700},
		{Kind: StepCreateDir, Target: paths.LogDir, Mode: 0o755},
		{Kind: StepCreateDir, Target: paths.StubDir, Mode: 0o755},
		{Kind: StepDownloadBinary, Target: paths.TeleproxyBin, URL: teleproxyDownloadURL(), SHA256: ""},
		{Kind: StepWriteFile, Target: paths.TeleproxyTOML, Content: tpData, Mode: 0o600},
		{Kind: StepWriteFile, Target: paths.TeleproxyService, Content: tpUnit.Render(), Mode: 0o644},
		{Kind: StepWriteFile, Target: "/etc/nginx/sites-available/tgproxy-stub", Content: ngData, Mode: 0o644},
		{Kind: StepWriteFile, Target: paths.StubDir + "/index.html", Content: minimalStubHTML(), Mode: 0o644},
		{Kind: StepWriteFile, Target: paths.PanelService, Content: panelUnit.Render(), Mode: 0o644},
		{Kind: StepEnableService, Target: "teleproxy"},
		{Kind: StepStartService, Target: "teleproxy"},
	}

	return Plan{
		Mode:  config.ModeSingle,
		Steps: steps,
		Creds: creds,
	}, nil
}

func teleproxyDownloadURL() string {
	return "https://github.com/teleproxy/teleproxy/releases/latest/download/teleproxy_linux_amd64"
}

func minimalStubHTML() []byte {
	return []byte(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>Welcome</title></head>
<body><h1>Welcome</h1></body>
</html>
`)
}
