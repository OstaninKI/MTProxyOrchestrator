package reconcile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/nginx"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/service"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/stub"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/systemd"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
)

const (
	stubTLSBackendPort = 9443
	panelBackendPort   = 18080
	bridgeSOCKS5Addr   = "127.0.0.1:1080"
)

type Options struct {
	ConfigFile string
	PanelDB    string
	Paths      config.InstallPaths
}

func Reconcile(opts Options) error {
	cfg, err := config.ReadConfig(opts.ConfigFile)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	rt, err := config.ReadRuntimeSettings(opts.PanelDB)
	if err != nil {
		return fmt.Errorf("read runtime settings: %w", err)
	}
	cfg = rt.MergeInto(cfg)

	effectiveMaskHost := cfg.MaskHost
	tlsBackend := cfg.TLSBackend
	hasTLSStubBackend := cfg.PanelDomain != "" && cfg.PanelCertPath != "" && cfg.PanelKeyPath != ""
	if hasTLSStubBackend {
		effectiveMaskHost = cfg.PanelDomain
		tlsBackend = fmt.Sprintf("127.0.0.1:%d", stubTLSBackendPort)
		cfg.MaskHost = effectiveMaskHost
		cfg.TLSBackend = tlsBackend
	}

	effectiveMode := cfg.Mode // already merged: DB bridge_mode wins if set in DB
	if rt.Mode == "" {
		// DB has no explicit bridge_mode: preserve live state across reconcile
		// (legacy installs and the very update that ships this fix).
		if detected, derr := teleproxy.DetectMode(opts.Paths.TeleproxyTOML); derr == nil {
			effectiveMode = detected
		}
	}

	socks5Addr := ""
	if effectiveMode == config.ModeBridge {
		socks5Addr = bridgeSOCKS5Addr
	}

	users, err := readUsers(opts.Paths.UsersJSON)
	if err != nil {
		return fmt.Errorf("read users: %w", err)
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
		Users:        users,
	}
	newTpCfg := tpCfg.Render()
	oldTpCfg, _ := os.ReadFile(opts.Paths.TeleproxyTOML)
	teleproxyChanged := !bytes.Equal(oldTpCfg, newTpCfg)
	if err := writeFile(opts.Paths.TeleproxyTOML, newTpCfg, 0o600); err != nil {
		return fmt.Errorf("write teleproxy config: %w", err)
	}

	tpUnit := systemd.TeleproxyUnitConfig{
		BinaryPath: opts.Paths.TeleproxyBin,
		ConfigPath: opts.Paths.TeleproxyTOML,
		LogPath:    opts.Paths.TeleproxyLog,
	}
	if err := writeFile(opts.Paths.TeleproxyService, tpUnit.Render(), 0o644); err != nil {
		return fmt.Errorf("write teleproxy unit: %w", err)
	}

	panelUnit := systemd.PanelUnitConfig{
		BinaryPath:    opts.Paths.PanelBin,
		ConfigPath:    opts.Paths.ConfigFile,
		DBPath:        opts.Paths.PanelDB,
		PanelPath:     cfg.PanelPath,
		ListenAddr:    fmt.Sprintf("127.0.0.1:%d", panelBackendPort),
		MTProtoPort:   cfg.MTProtoPort,
		MaskHost:      effectiveMaskHost,
		TLSBackend:    tlsBackend,
		WildcardMask:  cfg.WildcardMask,
		MSSClamp:      cfg.MSSClamp,
		RandomPadding: cfg.RandomPadding,
		JA4Log:        cfg.JA4Log,
		StatsPort:     9091,
		LogPath:       opts.Paths.PanelLog,
		ConfigDir:     opts.Paths.ConfigDir,
		LogDir:        opts.Paths.LogDir,
		BinDir:        opts.Paths.BinDir,
		SystemdDir:    opts.Paths.SystemdDir,
		StubDir:       opts.Paths.StubDir,
		CertDir:       opts.Paths.CertDir,
		Domain:        cfg.PanelDomain,
		ACMEEmail:     cfg.ACMEEmail,
	}
	newPanelUnit := panelUnit.Render()
	oldPanelUnit, _ := os.ReadFile(opts.Paths.PanelService)
	panelUnitChanged := !bytes.Equal(oldPanelUnit, newPanelUnit)
	if err := writeFile(opts.Paths.PanelService, newPanelUnit, 0o644); err != nil {
		return fmt.Errorf("write panel unit: %w", err)
	}

	if effectiveMode == config.ModeBridge {
		sbUnit := systemd.SingboxUnitConfig{
			BinaryPath: opts.Paths.SingboxBin,
			ConfigPath: opts.Paths.SingboxJSON,
			LogPath:    opts.Paths.SingboxLog,
		}
		if err := writeFile(opts.Paths.SingboxService, sbUnit.Render(), 0o644); err != nil {
			return fmt.Errorf("write sing-box unit: %w", err)
		}
	}

	nginxChanged := false

	// Raise worker_connections / worker_rlimit_nofile in the distro-owned
	// nginx.conf. These directives live in events{} and the main context, which
	// no sites-enabled/ or conf.d/ drop-in can reach, so we patch the main file
	// in place. The patch is idempotent and only raises values below our floor.
	if mainConf, readErr := os.ReadFile("/etc/nginx/nginx.conf"); readErr == nil {
		patched := nginx.PatchMainConf(mainConf)
		changed, err := writeFileIfChanged("/etc/nginx/nginx.conf", patched, 0o644)
		if err != nil {
			return fmt.Errorf("patch nginx.conf: %w", err)
		}
		nginxChanged = nginxChanged || changed
	}

	acmeSnippetPath := ""
	if cfg.ACMEEmail != "" && cfg.PanelDomain != "" {
		acmeSnippetPath = opts.Paths.NginxSnippetDir + "/acme-challenge.conf"
		acmeData := nginx.ACMEChallengeConfig{
			WebRootDir: opts.Paths.CertDir + "/.well-known-webroot",
		}.Render()
		changed, err := writeFileIfChanged(acmeSnippetPath, acmeData, 0o644)
		if err != nil {
			return fmt.Errorf("write acme snippet: %w", err)
		}
		nginxChanged = nginxChanged || changed
	}

	ngCfg := nginx.StubConfig{
		ListenPort:      80,
		ServerName:      "_",
		StubRoot:        opts.Paths.StubDir,
		ACMESnippetPath: acmeSnippetPath,
	}
	changed, err := writeFileIfChanged("/etc/nginx/sites-available/tgproxy-stub", ngCfg.Render(), 0o644)
	if err != nil {
		return fmt.Errorf("write nginx stub: %w", err)
	}
	nginxChanged = nginxChanged || changed
	enabled, err := ensureNginxSiteEnabled("tgproxy-stub")
	if err != nil {
		return fmt.Errorf("enable nginx stub: %w", err)
	}
	nginxChanged = nginxChanged || enabled

	if hasTLSStubBackend {
		tlsStub := nginx.TLSStubConfig{
			ListenPort: stubTLSBackendPort,
			ServerName: cfg.PanelDomain,
			StubRoot:   opts.Paths.StubDir,
			CertPath:   cfg.PanelCertPath,
			KeyPath:    cfg.PanelKeyPath,
		}
		changed, err := writeFileIfChanged("/etc/nginx/sites-available/tgproxy-stub-tls", tlsStub.Render(), 0o644)
		if err != nil {
			return fmt.Errorf("write nginx tls stub: %w", err)
		}
		nginxChanged = nginxChanged || changed
		enabled, err := ensureNginxSiteEnabled("tgproxy-stub-tls")
		if err != nil {
			return fmt.Errorf("enable nginx tls stub: %w", err)
		}
		nginxChanged = nginxChanged || enabled
	}

	if cfg.PanelDomain != "" && cfg.PanelCertPath != "" && cfg.PanelKeyPath != "" {
		panelProxy := nginx.PanelProxyConfig{
			ListenPort:  8443,
			Domain:      cfg.PanelDomain,
			CertPath:    cfg.PanelCertPath,
			KeyPath:     cfg.PanelKeyPath,
			BackendAddr: fmt.Sprintf("127.0.0.1:%d", panelBackendPort),
		}
		changed, err := writeFileIfChanged("/etc/nginx/sites-available/tgproxy-panel", panelProxy.Render(), 0o644)
		if err != nil {
			return fmt.Errorf("write nginx panel proxy: %w", err)
		}
		nginxChanged = nginxChanged || changed
		enabled, err = ensureNginxSiteEnabled("tgproxy-panel")
		if err != nil {
			return fmt.Errorf("enable nginx panel: %w", err)
		}
		nginxChanged = nginxChanged || enabled
	}

	stubMigrated, err := stub.MigrateStubIfLegacy(opts.Paths.StubDir + "/index.html")
	if err != nil {
		return fmt.Errorf("migrate stub: %w", err)
	}

	// Remove the Ubuntu default nginx site to prevent it from shadowing tgproxy-stub on port 80.
	if removeErr := os.Remove("/etc/nginx/sites-enabled/default"); removeErr == nil {
		nginxChanged = true
	}

	mgr := service.NewManager(opts.Paths)
	if err := mgr.DaemonReload(); err != nil {
		return fmt.Errorf("daemon reload: %w", err)
	}

	if teleproxyChanged {
		if err := mgr.Restart("teleproxy.service"); err != nil {
			return fmt.Errorf("restart teleproxy: %w", err)
		}
	}

	if panelUnitChanged {
		if err := mgr.Restart("tgproxy-panel.service"); err != nil {
			return fmt.Errorf("restart panel: %w", err)
		}
	}

	if nginxChanged || stubMigrated {
		if err := mgr.ReloadNginx(); err != nil {
			return fmt.Errorf("reload nginx: %w", err)
		}
	}

	return nil
}

func readUsers(path string) ([]teleproxy.UserEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return teleproxy.UnmarshalUsersJSON(data)
}

func ensureNginxSiteEnabled(name string) (bool, error) {
	src := filepath.Join("/etc/nginx/sites-available", name)
	dst := filepath.Join("/etc/nginx/sites-enabled", name)
	if _, err := os.Lstat(dst); err == nil {
		return false, nil
	}
	if err := os.Symlink(src, dst); err != nil {
		return false, err
	}
	return true, nil
}

func writeFileIfChanged(path string, data []byte, mode os.FileMode) (bool, error) {
	old, _ := os.ReadFile(path)
	if bytes.Equal(old, data) {
		return false, nil
	}
	return true, writeFile(path, data, mode)
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".reconcile-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
