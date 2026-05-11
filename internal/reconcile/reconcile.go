package reconcile

import (
	"bytes"
	"encoding/json"
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
	teleproxyDomain := cfg.MaskHost
	hasTLSStubBackend := cfg.PanelDomain != "" && cfg.PanelCertPath != "" && cfg.PanelKeyPath != ""
	if hasTLSStubBackend {
		effectiveMaskHost = cfg.PanelDomain
		teleproxyDomain = fmt.Sprintf("%s:%d", cfg.PanelDomain, stubTLSBackendPort)
		cfg.MaskHost = effectiveMaskHost
	}

	socks5Addr := ""
	if cfg.Mode == config.ModeBridge {
		socks5Addr = bridgeSOCKS5Addr
	}

	users, err := readUsers(opts.Paths.UsersJSON)
	if err != nil {
		return fmt.Errorf("read users: %w", err)
	}

	tpCfg := teleproxy.Config{
		Port:       cfg.MTProtoPort,
		MaskHost:   teleproxyDomain,
		StatsPort:  9091,
		SOCKS5Addr: socks5Addr,
		Users:      users,
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
		BinaryPath:  opts.Paths.PanelBin,
		ConfigPath:  opts.Paths.ConfigFile,
		DBPath:      opts.Paths.PanelDB,
		PanelPath:   cfg.PanelPath,
		ListenAddr:  fmt.Sprintf("127.0.0.1:%d", panelBackendPort),
		MTProtoPort: cfg.MTProtoPort,
		MaskHost:    effectiveMaskHost,
		StatsPort:   9091,
		LogPath:     opts.Paths.PanelLog,
		ConfigDir:   opts.Paths.ConfigDir,
		LogDir:      opts.Paths.LogDir,
		BinDir:      opts.Paths.BinDir,
		SystemdDir:  opts.Paths.SystemdDir,
		StubDir:     opts.Paths.StubDir,
		CertDir:     opts.Paths.CertDir,
		Domain:      cfg.PanelDomain,
		ACMEEmail:   cfg.ACMEEmail,
	}
	newPanelUnit := panelUnit.Render()
	oldPanelUnit, _ := os.ReadFile(opts.Paths.PanelService)
	panelUnitChanged := !bytes.Equal(oldPanelUnit, newPanelUnit)
	if err := writeFile(opts.Paths.PanelService, newPanelUnit, 0o644); err != nil {
		return fmt.Errorf("write panel unit: %w", err)
	}

	if cfg.Mode == config.ModeBridge {
		sbUnit := systemd.SingboxUnitConfig{
			BinaryPath: opts.Paths.SingboxBin,
			ConfigPath: opts.Paths.SingboxJSON,
			LogPath:    opts.Paths.SingboxLog,
		}
		if err := writeFile(opts.Paths.SingboxService, sbUnit.Render(), 0o644); err != nil {
			return fmt.Errorf("write sing-box unit: %w", err)
		}
	}

	acmeSnippetPath := ""
	if cfg.ACMEEmail != "" && cfg.PanelDomain != "" {
		acmeSnippetPath = opts.Paths.NginxSnippetDir + "/acme-challenge.conf"
		acmeData := nginx.ACMEChallengeConfig{
			WebRootDir: opts.Paths.CertDir + "/.well-known-webroot",
		}.Render()
		if err := writeFile(acmeSnippetPath, acmeData, 0o644); err != nil {
			return fmt.Errorf("write acme snippet: %w", err)
		}
	}

	ngCfg := nginx.StubConfig{
		ListenPort:      80,
		ServerName:      "_",
		StubRoot:        opts.Paths.StubDir,
		ACMESnippetPath: acmeSnippetPath,
	}
	if err := writeFile("/etc/nginx/sites-available/tgproxy-stub", ngCfg.Render(), 0o644); err != nil {
		return fmt.Errorf("write nginx stub: %w", err)
	}

	if hasTLSStubBackend {
		tlsStub := nginx.TLSStubConfig{
			ListenPort: stubTLSBackendPort,
			ServerName: cfg.PanelDomain,
			StubRoot:   opts.Paths.StubDir,
			CertPath:   cfg.PanelCertPath,
			KeyPath:    cfg.PanelKeyPath,
		}
		if err := writeFile("/etc/nginx/sites-available/tgproxy-stub-tls", tlsStub.Render(), 0o644); err != nil {
			return fmt.Errorf("write nginx tls stub: %w", err)
		}
	}

	if cfg.PanelDomain != "" && cfg.PanelCertPath != "" && cfg.PanelKeyPath != "" {
		panelProxy := nginx.PanelProxyConfig{
			ListenPort:  8443,
			Domain:      cfg.PanelDomain,
			CertPath:    cfg.PanelCertPath,
			KeyPath:     cfg.PanelKeyPath,
			BackendAddr: fmt.Sprintf("127.0.0.1:%d", panelBackendPort),
		}
		if err := writeFile("/etc/nginx/sites-available/tgproxy-panel", panelProxy.Render(), 0o644); err != nil {
			return fmt.Errorf("write nginx panel proxy: %w", err)
		}
	}

	if _, err := stub.MigrateStubIfLegacy(opts.Paths.StubDir + "/index.html"); err != nil {
		return fmt.Errorf("migrate stub: %w", err)
	}

	// Remove the Ubuntu default nginx site to prevent it from shadowing tgproxy-stub on port 80.
	_ = os.Remove("/etc/nginx/sites-enabled/default")

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

	if err := mgr.ReloadNginx(); err != nil {
		return fmt.Errorf("reload nginx: %w", err)
	}

	return nil
}

type usersFile struct {
	Users []teleproxy.UserEntry `json:"users"`
}

func readUsers(path string) ([]teleproxy.UserEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var uf usersFile
	if err := json.Unmarshal(data, &uf); err != nil {
		return nil, err
	}
	return uf.Users, nil
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
