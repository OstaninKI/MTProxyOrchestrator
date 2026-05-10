package bridge_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/singbox"
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

func (f *fakeExecutor) ReloadService(name string) error {
	if err := f.record("ReloadService:" + name); err != nil {
		return err
	}
	f.services[name] = true
	return nil
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

func TestEnableBridgeRollbackRestoresExistingOutboundsOnStartFailure(t *testing.T) {
	exec := newFakeExecutor()
	exec.failOn = "StartService:sing-box.service"
	cfg := testEnableCfg(t)
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	original := []byte("{\"nodes\":[{\"id\":7,\"type\":\"vless-reality\",\"tag\":\"old\",\"host\":\"old.example\",\"port\":443,\"uuid\":\"old-uuid\",\"sni\":\"old.example\",\"public_key\":\"old-pk\",\"short_id\":\"old-sid\",\"enabled\":true}]}\n")
	if err := os.WriteFile(nodePath, original, 0o600); err != nil {
		t.Fatalf("write original outbounds: %v", err)
	}
	svc := &bridge.BridgeService{Exec: exec, NodePath: nodePath}

	err := svc.Enable(cfg)
	if err == nil {
		t.Fatal("expected error when sing-box fails to start")
	}

	got, err := os.ReadFile(nodePath)
	if err != nil {
		t.Fatalf("read restored outbounds: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("outbounds.json not restored\nwant: %s\ngot:  %s", original, got)
	}
}

func TestEnableBridgeRollbackRemovesOutboundsWhenOriginallyMissing(t *testing.T) {
	exec := newFakeExecutor()
	exec.failOn = "StartService:sing-box.service"
	cfg := testEnableCfg(t)
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	svc := &bridge.BridgeService{Exec: exec, NodePath: nodePath}

	err := svc.Enable(cfg)
	if err == nil {
		t.Fatal("expected error when sing-box fails to start")
	}

	if _, err := os.Stat(nodePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outbounds.json should be absent after rollback, stat err=%v", err)
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

func TestEnableBridgeUsesConfiguredStrategy(t *testing.T) {
	cases := []struct {
		name      string
		strategy  singbox.Strategy
		wantType  string
		wantExtra string
	}{
		{name: "roundrobin", strategy: singbox.StrategyRoundRobin, wantType: "selector"},
		{name: "fallback", strategy: singbox.StrategyFallback, wantType: "urltest", wantExtra: "interrupt_exist_connections"},
		{name: "selector", strategy: singbox.StrategySelector, wantType: "selector"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := newFakeExecutor()
			cfg := testEnableCfg(t)
			cfg.Strategy = string(tc.strategy)
			dir := t.TempDir()
			nodePath := filepath.Join(dir, "outbounds.json")
			existing := bridge.NodeList{Nodes: []bridge.Node{
				{
					ID: 1, Type: bridge.NodeTypeVLESSReality, Tag: "n0",
					Host: "10.0.0.2", Port: 443, UUID: "some-uuid-0",
					SNI: "sni0.test", PublicKey: "pk0", ShortID: "sid0",
					Enabled: true,
				},
			}}
			if err := existing.Save(nodePath); err != nil {
				t.Fatalf("seed nodes: %v", err)
			}
			svc := &bridge.BridgeService{Exec: exec, NodePath: nodePath}

			if err := svc.Enable(cfg); err != nil {
				t.Fatalf("Enable: %v", err)
			}

			g := renderedProxyGroup(t, exec.written[cfg.Paths.SingboxJSON])
			if g["type"] != tc.wantType {
				t.Fatalf("strategy %s rendered type=%v, want %s", tc.strategy, g["type"], tc.wantType)
			}
			if tc.wantExtra != "" {
				if _, ok := g[tc.wantExtra]; !ok {
					t.Fatalf("strategy %s missing %s in rendered group: %#v", tc.strategy, tc.wantExtra, g)
				}
			}

			nl, err := bridge.Load(nodePath)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if nl.Strategy != string(tc.strategy) {
				t.Fatalf("persisted strategy=%q, want %q", nl.Strategy, tc.strategy)
			}
		})
	}
}

func TestRerenderRoundRobinRotatesOutboundOrderAndPersistsCursor(t *testing.T) {
	exec := newFakeExecutor()
	dir := t.TempDir()
	nodePath := filepath.Join(dir, "outbounds.json")
	jsonPath := filepath.Join(dir, "sing-box.json")
	nl := bridge.NodeList{
		Strategy: "roundrobin",
		Nodes: []bridge.Node{
			{ID: 1, Type: bridge.NodeTypeVLESSReality, Tag: "node-a", Host: "1.1.1.1", Port: 443, UUID: "uuid-a", SNI: "a.example.com", PublicKey: "pk-a", Enabled: true},
			{ID: 2, Type: bridge.NodeTypeVLESSReality, Tag: "node-b", Host: "2.2.2.2", Port: 443, UUID: "uuid-b", SNI: "b.example.com", PublicKey: "pk-b", Enabled: true},
			{ID: 3, Type: bridge.NodeTypeVLESSReality, Tag: "node-c", Host: "3.3.3.3", Port: 443, UUID: "uuid-c", SNI: "c.example.com", PublicKey: "pk-c", Enabled: true},
		},
	}
	if err := nl.Save(nodePath); err != nil {
		t.Fatalf("seed nodes: %v", err)
	}
	svc := &bridge.BridgeService{Exec: exec, NodePath: nodePath}

	if err := svc.RerenderConfig(nl, jsonPath); err != nil {
		t.Fatalf("first RerenderConfig: %v", err)
	}
	first := outboundTags(t, exec.written[jsonPath])

	updated, err := bridge.Load(nodePath)
	if err != nil {
		t.Fatalf("load updated nodes: %v", err)
	}
	if updated.RoundRobinCursor != 1 {
		t.Fatalf("after first render cursor=%d, want 1", updated.RoundRobinCursor)
	}

	if err := svc.RerenderConfig(updated, jsonPath); err != nil {
		t.Fatalf("second RerenderConfig: %v", err)
	}
	second := outboundTags(t, exec.written[jsonPath])
	if first[0] == second[0] {
		t.Fatalf("round-robin did not rotate first outbound: first=%v second=%v", first, second)
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

func renderedProxyGroup(t *testing.T, out []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal rendered sing-box config: %v\n%s", err, out)
	}
	for _, raw := range doc["outbounds"].([]any) {
		ob := raw.(map[string]any)
		if ob["tag"] == "proxy" {
			return ob
		}
	}
	t.Fatal("proxy group not found")
	return nil
}

func outboundTags(t *testing.T, out []byte) []string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal rendered sing-box config: %v\n%s", err, out)
	}
	var tags []string
	for _, raw := range doc["outbounds"].([]any) {
		ob := raw.(map[string]any)
		if ob["tag"] == "proxy" || ob["tag"] == "direct" {
			continue
		}
		tags = append(tags, ob["tag"].(string))
	}
	return tags
}
