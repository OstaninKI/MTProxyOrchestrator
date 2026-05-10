package bridge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/singbox"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/systemd"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
)

const (
	singboxSOCKSAddr = "127.0.0.1"
	singboxSOCKSPort = 1080
)

// Executor is the set of OS operations needed by BridgeService.
// All methods accept injectable implementations so tests never touch the real system.
type Executor interface {
	WriteFile(path string, data []byte, mode os.FileMode) error
	Download(url, sha256hex, destPath string) error
	EnableService(name string) error
	StartService(name string) error
	StopService(name string) error
	DisableService(name string) error
	ReloadService(name string) error
	ServiceActive(name string) (bool, error)
}

// EnableConfig carries all parameters needed to enable Bridge mode.
type EnableConfig struct {
	Node           Node
	Paths          config.InstallPaths
	TeleproxyUsers []teleproxy.UserEntry
	MTProtoPort    int
	MaskHost       string
	StatsPort      int
	Strategy       string
	SingboxURL     string
	SingboxSHA256  string
}

// DisableConfig carries parameters needed to disable Bridge mode.
type DisableConfig struct {
	Paths          config.InstallPaths
	TeleproxyUsers []teleproxy.UserEntry
	MTProtoPort    int
	MaskHost       string
	StatsPort      int
}

// BridgeService manages Bridge mode transitions.
type BridgeService struct {
	Exec     Executor
	NodePath string // path to outbounds.json
}

type nodeFileSnapshot struct {
	existed bool
	data    []byte
	mode    os.FileMode
}

// Enable switches from Single to Bridge mode:
//  1. Saves the new node to NodePath.
//  2. Downloads and verifies sing-box binary (if URL provided).
//  3. Renders sing-box.json and sing-box.service.
//  4. Re-renders teleproxy.toml with SOCKS5 upstream.
//  5. Enables and starts sing-box, then restarts teleproxy.
//  6. Verifies both services are active; rolls back on failure.
func (s *BridgeService) Enable(cfg EnableConfig) error {
	// 1. Persist node list.
	nl, err := Load(s.NodePath)
	if err != nil {
		return fmt.Errorf("bridge enable: load nodes: %w", err)
	}
	nodeSnapshot, err := snapshotNodeFile(s.NodePath)
	if err != nil {
		return fmt.Errorf("bridge enable: snapshot nodes: %w", err)
	}
	node := cfg.Node
	node.ID = nl.NextID()
	node.Enabled = true
	nl.Nodes = append(nl.Nodes, node)
	if cfg.Strategy != "" {
		nl.Strategy = string(normalizeStrategy(cfg.Strategy))
	}
	if err := nl.Save(s.NodePath); err != nil {
		return fmt.Errorf("bridge enable: save nodes: %w", err)
	}

	// 2. Download sing-box binary if a URL was provided.
	if cfg.SingboxURL != "" {
		if err := s.Exec.Download(cfg.SingboxURL, cfg.SingboxSHA256, cfg.Paths.SingboxBin); err != nil {
			return s.rollback(cfg, fmt.Errorf("bridge enable: download sing-box: %w", err), nodeSnapshot)
		}
	}

	// 3. Render and write sing-box.json.
	strategy := normalizeStrategy(nl.Strategy)
	outbounds, renderedNL := renderOutbounds(nl, strategy)
	sbCfg := singbox.Config{
		SOCKSListenAddr: singboxSOCKSAddr,
		SOCKSListenPort: singboxSOCKSPort,
		Strategy:        strategy,
		Outbounds:       outbounds,
	}
	sbJSON, err := sbCfg.Render()
	if err != nil {
		return s.rollback(cfg, fmt.Errorf("bridge enable: render sing-box.json: %w", err), nodeSnapshot)
	}
	if err := s.Exec.WriteFile(cfg.Paths.SingboxJSON, sbJSON, 0o600); err != nil {
		return s.rollback(cfg, fmt.Errorf("bridge enable: write sing-box.json: %w", err), nodeSnapshot)
	}
	if renderedNL.RoundRobinCursor != nl.RoundRobinCursor {
		if err := renderedNL.Save(s.NodePath); err != nil {
			return s.rollback(cfg, fmt.Errorf("bridge enable: save round-robin cursor: %w", err), nodeSnapshot)
		}
	}

	// 4. Render and write sing-box.service.
	sbUnit := systemd.SingboxUnitConfig{
		BinaryPath: cfg.Paths.SingboxBin,
		ConfigPath: cfg.Paths.SingboxJSON,
		LogPath:    cfg.Paths.SingboxLog,
	}
	if err := s.Exec.WriteFile(cfg.Paths.SingboxService, sbUnit.Render(), 0o644); err != nil {
		return s.rollback(cfg, fmt.Errorf("bridge enable: write sing-box.service: %w", err), nodeSnapshot)
	}

	// 5. Re-render teleproxy.toml with SOCKS5 upstream.
	tpCfg := teleproxy.Config{
		Port:       cfg.MTProtoPort,
		MaskHost:   cfg.MaskHost,
		StatsPort:  cfg.StatsPort,
		SOCKS5Addr: fmt.Sprintf("%s:%d", singboxSOCKSAddr, singboxSOCKSPort),
		Users:      cfg.TeleproxyUsers,
	}
	if err := s.Exec.WriteFile(cfg.Paths.TeleproxyTOML, tpCfg.Render(), 0o600); err != nil {
		return s.rollback(cfg, fmt.Errorf("bridge enable: write teleproxy.toml: %w", err), nodeSnapshot)
	}

	// 6. Enable and start sing-box, then reload-or-restart teleproxy.
	if err := s.Exec.EnableService("sing-box.service"); err != nil {
		return s.rollback(cfg, fmt.Errorf("bridge enable: enable sing-box: %w", err), nodeSnapshot)
	}
	if err := s.Exec.StartService("sing-box.service"); err != nil {
		return s.rollback(cfg, fmt.Errorf("bridge enable: start sing-box: %w", err), nodeSnapshot)
	}
	if err := s.Exec.ReloadService("teleproxy.service"); err != nil {
		return s.rollback(cfg, fmt.Errorf("bridge enable: reload teleproxy: %w", err), nodeSnapshot)
	}

	// 7. Health check: both services must be active.
	sbActive, err := s.Exec.ServiceActive("sing-box.service")
	if err != nil || !sbActive {
		return s.rollback(cfg, fmt.Errorf("bridge enable: sing-box not active after start"), nodeSnapshot)
	}
	tpActive, err := s.Exec.ServiceActive("teleproxy.service")
	if err != nil || !tpActive {
		return s.rollback(cfg, fmt.Errorf("bridge enable: teleproxy not active after restart"), nodeSnapshot)
	}

	return nil
}

// Disable switches from Bridge back to Single mode:
//  1. Snapshots teleproxy.toml and sing-box active state.
//  2. Stops and disables sing-box.
//  3. Re-renders teleproxy.toml in direct (no SOCKS5) mode.
//  4. Reloads teleproxy.
//
// On failure at write/reload, the previous teleproxy.toml is restored,
// teleproxy is reloaded back, and sing-box is restarted if it was active.
// Node list is preserved; nodes remain for future re-enable.
func (s *BridgeService) Disable(cfg DisableConfig) error {
	tpSnap, snapErr := snapshotFile(cfg.Paths.TeleproxyTOML)
	if snapErr != nil {
		return fmt.Errorf("bridge disable: snapshot teleproxy.toml: %w", snapErr)
	}
	sbWasActive, _ := s.Exec.ServiceActive("sing-box.service")

	if err := s.Exec.StopService("sing-box.service"); err != nil {
		return fmt.Errorf("bridge disable: stop sing-box: %w", err)
	}
	if err := s.Exec.DisableService("sing-box.service"); err != nil {
		return fmt.Errorf("bridge disable: disable sing-box: %w", err)
	}

	tpCfg := teleproxy.Config{
		Port:      cfg.MTProtoPort,
		MaskHost:  cfg.MaskHost,
		StatsPort: cfg.StatsPort,
		Users:     cfg.TeleproxyUsers,
	}
	if err := s.Exec.WriteFile(cfg.Paths.TeleproxyTOML, tpCfg.Render(), 0o600); err != nil {
		return s.disableRollback(cfg, tpSnap, sbWasActive, fmt.Errorf("bridge disable: write teleproxy.toml: %w", err))
	}

	if err := s.Exec.ReloadService("teleproxy.service"); err != nil {
		return s.disableRollback(cfg, tpSnap, sbWasActive, fmt.Errorf("bridge disable: reload teleproxy: %w", err))
	}

	active, err := s.Exec.ServiceActive("teleproxy.service")
	if err != nil || !active {
		return s.disableRollback(cfg, tpSnap, sbWasActive, fmt.Errorf("bridge disable: teleproxy not active after restart"))
	}
	return nil
}

// disableRollback restores teleproxy.toml from snapshot, reloads teleproxy,
// and restarts sing-box if it was active before. Best-effort: rollback errors
// are joined to the original cause but do not mask it.
func (s *BridgeService) disableRollback(cfg DisableConfig, tpSnap fileSnapshot, sbWasActive bool, cause error) error {
	var rollbackErrs []error
	if tpSnap.existed {
		if err := s.Exec.WriteFile(cfg.Paths.TeleproxyTOML, tpSnap.data, tpSnap.mode); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restore teleproxy.toml: %w", err))
		} else if err := s.Exec.ReloadService("teleproxy.service"); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("reload teleproxy: %w", err))
		}
	}
	if sbWasActive {
		if err := s.Exec.StartService("sing-box.service"); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("restart sing-box: %w", err))
		}
	}
	if len(rollbackErrs) > 0 {
		return fmt.Errorf("bridge disable rolled back with errors: %w: %w", cause, errors.Join(rollbackErrs...))
	}
	return fmt.Errorf("bridge disable rolled back: %w", cause)
}

// rollback restores teleproxy.toml to Single (direct) mode, stops sing-box,
// and returns a combined error.
func (s *BridgeService) rollback(cfg EnableConfig, cause error, nodeSnapshot nodeFileSnapshot) error {
	var rollbackErrs []error
	if err := restoreNodeFile(s.NodePath, nodeSnapshot); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("restore nodes: %w", err))
	}
	tpCfg := teleproxy.Config{
		Port:      cfg.MTProtoPort,
		MaskHost:  cfg.MaskHost,
		StatsPort: cfg.StatsPort,
		Users:     cfg.TeleproxyUsers,
	}
	if err := s.Exec.WriteFile(cfg.Paths.TeleproxyTOML, tpCfg.Render(), 0o600); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("restore teleproxy.toml: %w", err))
	}
	if err := s.Exec.StopService("sing-box.service"); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("stop sing-box: %w", err))
	}
	if err := s.Exec.DisableService("sing-box.service"); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("disable sing-box: %w", err))
	}
	if err := s.Exec.ReloadService("teleproxy.service"); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("reload teleproxy: %w", err))
	}
	if len(rollbackErrs) > 0 {
		return fmt.Errorf("bridge enable rolled back with errors: %w: %w", cause, errors.Join(rollbackErrs...))
	}
	return fmt.Errorf("bridge enable rolled back: %w", cause)
}

// nodeToOutbound maps a bridge Node to a singbox.Outbound descriptor.
func nodeToOutbound(n Node) singbox.Outbound {
	ob := singbox.Outbound{
		Tag:               n.Tag,
		Server:            n.Host,
		Port:              n.Port,
		TLSServer:         n.SNI,
		UUID:              n.UUID,
		Flow:              n.Flow,
		PublicKey:         n.PublicKey,
		ShortID:           n.ShortID,
		Password:          n.Password,
		Method:            n.Method,
		CongestionControl: n.CongestionControl,
	}
	switch n.Type {
	case NodeTypeTrojan:
		ob.Type = singbox.OutboundTrojan
	case NodeTypeShadowsocks:
		ob.Type = singbox.OutboundShadowsocks
	case NodeTypeHysteria2:
		ob.Type = singbox.OutboundHysteria2
	case NodeTypeTUIC:
		ob.Type = singbox.OutboundTUIC
	default:
		ob.Type = singbox.OutboundVLESSReality
	}
	return ob
}

// RerenderConfig re-renders and writes sing-box.json based on the current node list.
// If render or write fails, the previous sing-box.json content is restored.
// Returns a descriptive error for display to the admin; never includes node credentials.
func (s *BridgeService) RerenderConfig(nl NodeList, singboxJSONPath string) error {
	if len(nl.Active()) == 0 {
		return errors.New("no enabled nodes — cannot render sing-box config")
	}

	strategy := normalizeStrategy(nl.Strategy)
	outbounds, renderedNL := renderOutbounds(nl, strategy)

	cfg := singbox.Config{
		SOCKSListenAddr: singboxSOCKSAddr,
		SOCKSListenPort: singboxSOCKSPort,
		Strategy:        strategy,
		Outbounds:       outbounds,
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("sing-box config invalid: %w", err)
	}
	data, err := cfg.Render()
	if err != nil {
		return fmt.Errorf("sing-box config render failed: %w", err)
	}

	// Snapshot current sing-box.json for rollback.
	snap, snapErr := snapshotFile(singboxJSONPath)

	if err := s.Exec.WriteFile(singboxJSONPath, data, 0o600); err != nil {
		if snapErr == nil && snap.existed {
			_ = restoreFile(singboxJSONPath, snap)
		}
		return fmt.Errorf("sing-box config write failed: %w", err)
	}
	if renderedNL.RoundRobinCursor != nl.RoundRobinCursor {
		if err := renderedNL.Save(s.NodePath); err != nil {
			if snapErr == nil && snap.existed {
				_ = restoreFile(singboxJSONPath, snap)
			}
			return fmt.Errorf("round-robin cursor save failed: %w", err)
		}
	}
	return nil
}

func normalizeStrategy(strategy string) singbox.Strategy {
	switch singbox.Strategy(strategy) {
	case singbox.StrategyURLTest, singbox.StrategyFallback, singbox.StrategyRoundRobin, singbox.StrategySelector:
		return singbox.Strategy(strategy)
	default:
		return singbox.StrategyURLTest
	}
}

func renderOutbounds(nl NodeList, strategy singbox.Strategy) ([]singbox.Outbound, NodeList) {
	active := nl.Active()
	if strategy == singbox.StrategyRoundRobin && len(active) > 1 {
		offset := nl.RoundRobinCursor % len(active)
		if offset < 0 {
			offset = 0
		}
		rotated := append([]Node(nil), active[offset:]...)
		rotated = append(rotated, active[:offset]...)
		active = rotated
		nl.RoundRobinCursor = (offset + 1) % len(active)
	}

	outbounds := make([]singbox.Outbound, 0, len(active))
	for _, n := range active {
		outbounds = append(outbounds, nodeToOutbound(n))
	}
	return outbounds, nl
}

type fileSnapshot struct {
	existed bool
	data    []byte
	mode    os.FileMode
}

func snapshotFile(path string) (fileSnapshot, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileSnapshot{}, nil
	}
	if err != nil {
		return fileSnapshot{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{existed: true, data: data, mode: info.Mode().Perm()}, nil
}

func restoreFile(path string, snap fileSnapshot) error {
	if !snap.existed {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".rollback"
	if err := os.WriteFile(tmp, snap.data, snap.mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func snapshotNodeFile(path string) (nodeFileSnapshot, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nodeFileSnapshot{}, nil
	}
	if err != nil {
		return nodeFileSnapshot{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nodeFileSnapshot{}, err
	}
	return nodeFileSnapshot{existed: true, data: data, mode: info.Mode().Perm()}, nil
}

func restoreNodeFile(path string, snap nodeFileSnapshot) error {
	if !snap.existed {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".rollback"
	if err := os.WriteFile(tmp, snap.data, snap.mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
