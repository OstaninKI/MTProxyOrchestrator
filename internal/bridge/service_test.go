package bridge_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
)

// fakeExecutor records calls and can simulate failures.
type fakeExecutor struct {
	written  map[string][]byte
	services map[string]bool // active state
	calls    []string
	failOn   string // method:arg to fail
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{
		written:  make(map[string][]byte),
		services: make(map[string]bool),
	}
}

func (f *fakeExecutor) record(call string) error {
	f.calls = append(f.calls, call)
	if f.failOn == call {
		return errors.New("injected failure: " + call)
	}
	return nil
}

func (f *fakeExecutor) WriteFile(path string, data []byte, _ os.FileMode) error {
	if err := f.record("WriteFile:" + path); err != nil {
		return err
	}
	f.written[path] = data
	return nil
}

func (f *fakeExecutor) Download(url, _, dest string) error {
	return f.record("Download:" + dest)
}

func (f *fakeExecutor) EnableService(name string) error {
	return f.record("EnableService:" + name)
}

func (f *fakeExecutor) StartService(name string) error {
	if err := f.record("StartService:" + name); err != nil {
		return err
	}
	f.services[name] = true
	return nil
}

func (f *fakeExecutor) StopService(name string) error {
	if err := f.record("StopService:" + name); err != nil {
		return err
	}
	f.services[name] = false
	return nil
}

func (f *fakeExecutor) DisableService(name string) error {
	return f.record("DisableService:" + name)
}

func (f *fakeExecutor) ServiceActive(name string) (bool, error) {
	f.record("ServiceActive:" + name) //nolint:errcheck
	return f.services[name], nil
}

func testPaths(t *testing.T) config.InstallPaths {
	t.Helper()
	dir := t.TempDir()
	return config.InstallPaths{
		SingboxBin:     filepath.Join(dir, "sing-box"),
		SingboxJSON:    filepath.Join(dir, "sing-box.json"),
		SingboxService: filepath.Join(dir, "sing-box.service"),
		SingboxLog:     filepath.Join(dir, "sing-box.log"),
		TeleproxyTOML:  filepath.Join(dir, "teleproxy.toml"),
	}
}

func testEnableCfg(t *testing.T) bridge.EnableConfig {
	t.Helper()
	return bridge.EnableConfig{
		Node: bridge.Node{
			Type: bridge.NodeTypeVLESSReality, Tag: "n1",
			Host: "10.0.0.1", Port: 443, UUID: "some-uuid",
			SNI: "sni.test", PublicKey: "pk", ShortID: "sid",
			Enabled: true,
		},
		Paths:          testPaths(t),
		TeleproxyUsers: []teleproxy.UserEntry{{Label: "alice", Secret: "aabbccddeeff00112233445566778899"}},
		MTProtoPort:    443,
		MaskHost:       "www.microsoft.com",
		StatsPort:      9091,
		SingboxURL:     "", // no download in tests
	}
}

func TestEnableBridgeHappyPath(t *testing.T) {
	exec := newFakeExecutor()
	cfg := testEnableCfg(t)
	dir := t.TempDir()
	svc := &bridge.BridgeService{Exec: exec, NodePath: filepath.Join(dir, "outbounds.json")}

	if err := svc.Enable(cfg); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	// sing-box.json must be written with socks inbound
	if _, ok := exec.written[cfg.Paths.SingboxJSON]; !ok {
		t.Error("sing-box.json was not written")
	}
	// teleproxy.toml must be written with socks5 addr
	tp, ok := exec.written[cfg.Paths.TeleproxyTOML]
	if !ok {
		t.Fatal("teleproxy.toml was not written")
	}
	if !contains(tp, "socks5") {
		t.Errorf("teleproxy.toml should contain socks5, got:\n%s", tp)
	}
	// sing-box.service must be enabled and started
	if !containsCall(exec.calls, "EnableService:sing-box.service") {
		t.Error("sing-box.service was not enabled")
	}
	if !containsCall(exec.calls, "StartService:sing-box.service") {
		t.Error("sing-box.service was not started")
	}
}

func TestEnableBridgeNoSingboxDownloadWhenURLEmpty(t *testing.T) {
	exec := newFakeExecutor()
	cfg := testEnableCfg(t)
	cfg.SingboxURL = ""
	dir := t.TempDir()
	svc := &bridge.BridgeService{Exec: exec, NodePath: filepath.Join(dir, "outbounds.json")}

	if err := svc.Enable(cfg); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	for _, c := range exec.calls {
		if len(c) > 9 && c[:9] == "Download:" {
			t.Errorf("unexpected Download call: %s", c)
		}
	}
}

func TestEnableBridgeRollbackOnStartFailure(t *testing.T) {
	exec := newFakeExecutor()
	exec.failOn = "StartService:sing-box.service"
	cfg := testEnableCfg(t)
	dir := t.TempDir()
	svc := &bridge.BridgeService{Exec: exec, NodePath: filepath.Join(dir, "outbounds.json")}

	err := svc.Enable(cfg)
	if err == nil {
		t.Fatal("expected error when sing-box fails to start")
	}

	// rollback must stop sing-box and restart teleproxy
	if !containsCall(exec.calls, "StopService:sing-box.service") {
		t.Error("rollback did not stop sing-box")
	}
	if !containsCall(exec.calls, "DisableService:sing-box.service") {
		t.Error("rollback did not disable sing-box")
	}
	// teleproxy.toml should have been re-written without socks5
	if tp, ok := exec.written[cfg.Paths.TeleproxyTOML]; ok {
		if contains(tp, "socks5") {
			t.Error("rollback teleproxy.toml should not contain socks5")
		}
	}
}

func TestEnableBridgePersistsNode(t *testing.T) {
	exec := newFakeExecutor()
	cfg := testEnableCfg(t)
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	svc := &bridge.BridgeService{Exec: exec, NodePath: nodePath}

	if err := svc.Enable(cfg); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	nl, err := bridge.Load(nodePath)
	if err != nil {
		t.Fatalf("Load after Enable: %v", err)
	}
	if len(nl.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nl.Nodes))
	}
	if nl.Nodes[0].Tag != "n1" {
		t.Errorf("want tag=n1, got %q", nl.Nodes[0].Tag)
	}
}

func TestDisableBridgeHappyPath(t *testing.T) {
	exec := newFakeExecutor()
	exec.services["sing-box.service"] = true
	exec.services["teleproxy.service"] = true

	cfg := bridge.DisableConfig{
		Paths:          testPaths(t),
		TeleproxyUsers: []teleproxy.UserEntry{{Label: "alice", Secret: "aabbccddeeff00112233445566778899"}},
		MTProtoPort:    443,
		MaskHost:       "www.microsoft.com",
		StatsPort:      9091,
	}
	dir := t.TempDir()
	svc := &bridge.BridgeService{Exec: exec, NodePath: filepath.Join(dir, "outbounds.json")}

	if err := svc.Disable(cfg); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if !containsCall(exec.calls, "StopService:sing-box.service") {
		t.Error("sing-box was not stopped")
	}
	if !containsCall(exec.calls, "DisableService:sing-box.service") {
		t.Error("sing-box was not disabled")
	}
	tp, ok := exec.written[cfg.Paths.TeleproxyTOML]
	if !ok {
		t.Fatal("teleproxy.toml not written after disable")
	}
	if contains(tp, "socks5") {
		t.Error("teleproxy.toml after disable should not have socks5")
	}
}

func TestSingleToBridgeConfigTransition(t *testing.T) {
	exec := newFakeExecutor()
	cfg := testEnableCfg(t)
	dir := t.TempDir()
	svc := &bridge.BridgeService{Exec: exec, NodePath: filepath.Join(dir, "outbounds.json")}

	// Before enable: teleproxy should be Single (no socks5)
	disableCfg := bridge.DisableConfig{
		Paths: cfg.Paths, TeleproxyUsers: cfg.TeleproxyUsers,
		MTProtoPort: cfg.MTProtoPort, MaskHost: cfg.MaskHost, StatsPort: cfg.StatsPort,
	}
	_ = svc.Disable(disableCfg)
	tpBefore := exec.written[cfg.Paths.TeleproxyTOML]
	if contains(tpBefore, "socks5") {
		t.Error("single mode teleproxy.toml should not have socks5")
	}

	// After enable: teleproxy should have socks5
	exec2 := newFakeExecutor()
	svc2 := &bridge.BridgeService{Exec: exec2, NodePath: filepath.Join(dir, "outbounds.json")}
	if err := svc2.Enable(cfg); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	tpAfter := exec2.written[cfg.Paths.TeleproxyTOML]
	if !contains(tpAfter, "socks5") {
		t.Error("bridge mode teleproxy.toml should have socks5")
	}
}

func TestBridgeToSingleConfigTransition(t *testing.T) {
	dir := t.TempDir()

	// Enable first
	exec := newFakeExecutor()
	enableCfg := testEnableCfg(t)
	svc := &bridge.BridgeService{Exec: exec, NodePath: filepath.Join(dir, "outbounds.json")}
	if err := svc.Enable(enableCfg); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	// Now disable
	exec2 := newFakeExecutor()
	exec2.services["sing-box.service"] = true
	exec2.services["teleproxy.service"] = true
	svc2 := &bridge.BridgeService{Exec: exec2, NodePath: filepath.Join(dir, "outbounds.json")}
	disableCfg := bridge.DisableConfig{
		Paths: enableCfg.Paths, TeleproxyUsers: enableCfg.TeleproxyUsers,
		MTProtoPort: enableCfg.MTProtoPort, MaskHost: enableCfg.MaskHost, StatsPort: enableCfg.StatsPort,
	}
	if err := svc2.Disable(disableCfg); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	tpSingle := exec2.written[enableCfg.Paths.TeleproxyTOML]
	if contains(tpSingle, "socks5") {
		t.Error("after bridge→single, teleproxy.toml must not have socks5")
	}
}

func contains(data []byte, sub string) bool {
	return len(data) > 0 && indexOf(data, sub) >= 0
}

func indexOf(data []byte, sub string) int {
	s := string(data)
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func containsCall(calls []string, call string) bool {
	for _, c := range calls {
		if c == call {
			return true
		}
	}
	return false
}
