package singbox_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/singbox"
)

const updateGolden = false

var testNode = singbox.Outbound{
	Type:      singbox.OutboundVLESSReality,
	Tag:       "node-1",
	Server:    "198.51.100.1",
	Port:      443,
	UUID:      "12345678-1234-1234-1234-123456789012",
	Flow:      "xtls-rprx-vision",
	TLSServer: "www.example.com",
	PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
	ShortID:   "aabbccdd",
}

func baseCfg() singbox.Config {
	return singbox.Config{
		SOCKSListenAddr: "127.0.0.1",
		SOCKSListenPort: 2080,
		Strategy:        singbox.StrategyURLTest,
		Outbounds:       []singbox.Outbound{testNode},
	}
}

func TestRenderValidJSON(t *testing.T) {
	out, err := baseCfg().Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	var parsed any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
}

func TestRenderSOCKSBoundToLoopback(t *testing.T) {
	out, err := baseCfg().Render()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	inbounds := doc["inbounds"].([]any)
	if len(inbounds) == 0 {
		t.Fatal("no inbounds in output")
	}
	ib := inbounds[0].(map[string]any)
	if ib["listen"] != "127.0.0.1" {
		t.Errorf("SOCKS listen = %v, want 127.0.0.1", ib["listen"])
	}
}

func TestRenderGolden(t *testing.T) {
	out, err := baseCfg().Render()
	if err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	path := filepath.Join("testdata", "bridge_base.json")
	if updateGolden {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, out, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Log("updated golden file")
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set updateGolden=true to generate)", err)
	}
	if string(out) != string(want) {
		t.Errorf("render mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, out)
	}
}

func TestRenderNoFlowWhenEmpty(t *testing.T) {
	cfg := singbox.Config{
		SOCKSListenAddr: "127.0.0.1",
		SOCKSListenPort: 2080,
		Strategy:        singbox.StrategyURLTest,
		Outbounds: []singbox.Outbound{{
			Type:      singbox.OutboundVLESSReality,
			Tag:       "n1",
			Server:    "1.2.3.4",
			Port:      443,
			UUID:      "00000000-0000-0000-0000-000000000000",
			TLSServer: "ex.com",
			PublicKey: "AAAA=",
			ShortID:   "aa",
			// Flow intentionally empty
		}},
	}
	out, err := cfg.Render()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, out)
	}
	outbounds := doc["outbounds"].([]any)
	ob := outbounds[0].(map[string]any)
	if _, ok := ob["flow"]; ok {
		t.Error("flow key must be absent when Flow is empty")
	}
}

func TestRenderTrojanOutbound(t *testing.T) {
	cfg := singbox.Config{
		SOCKSListenAddr: "127.0.0.1",
		SOCKSListenPort: 2080,
		Strategy:        singbox.StrategyURLTest,
		Outbounds: []singbox.Outbound{{
			Type:      singbox.OutboundTrojan,
			Tag:       "trojan-node",
			Server:    "1.2.3.4",
			Port:      443,
			TLSServer: "example.com",
			Password:  "secret123",
		}},
	}
	out, err := cfg.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"type": "trojan"`) {
		t.Errorf("rendered JSON should contain \"type\": \"trojan\"\n%s", out)
	}
	if !strings.Contains(string(out), `"password": "secret123"`) {
		t.Errorf("rendered JSON should contain the password\n%s", out)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestRenderShadowsocksOutbound(t *testing.T) {
	cfg := singbox.Config{
		SOCKSListenAddr: "127.0.0.1",
		SOCKSListenPort: 2080,
		Strategy:        singbox.StrategyURLTest,
		Outbounds: []singbox.Outbound{{
			Type:     singbox.OutboundShadowsocks,
			Tag:      "ss-node",
			Server:   "5.6.7.8",
			Port:     8388,
			Method:   "2022-blake3-aes-128-gcm",
			Password: "sspassword",
		}},
	}
	out, err := cfg.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"type": "shadowsocks"`) {
		t.Errorf("rendered JSON should contain \"type\": \"shadowsocks\"\n%s", out)
	}
	if !strings.Contains(string(out), `"method": "2022-blake3-aes-128-gcm"`) {
		t.Errorf("rendered JSON should contain the method\n%s", out)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestRenderHysteria2Outbound(t *testing.T) {
	cfg := singbox.Config{
		SOCKSListenAddr: "127.0.0.1",
		SOCKSListenPort: 2080,
		Strategy:        singbox.StrategyURLTest,
		Outbounds: []singbox.Outbound{{
			Type:      singbox.OutboundHysteria2,
			Tag:       "hy2-node",
			Server:    "9.10.11.12",
			Port:      443,
			TLSServer: "hy2.example.com",
			Password:  "hy2secret",
		}},
	}
	out, err := cfg.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"type": "hysteria2"`) {
		t.Errorf("rendered JSON should contain \"type\": \"hysteria2\"\n%s", out)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestRenderTUICOutbound(t *testing.T) {
	cfg := singbox.Config{
		SOCKSListenAddr: "127.0.0.1",
		SOCKSListenPort: 2080,
		Strategy:        singbox.StrategyURLTest,
		Outbounds: []singbox.Outbound{{
			Type:              singbox.OutboundTUIC,
			Tag:               "tuic-node",
			Server:            "13.14.15.16",
			Port:              443,
			TLSServer:         "tuic.example.com",
			UUID:              "aaaabbbb-cccc-dddd-eeee-ffffffffffff",
			Password:          "tuicpass",
			CongestionControl: "bbr",
		}},
	}
	out, err := cfg.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"type": "tuic"`) {
		t.Errorf("rendered JSON should contain \"type\": \"tuic\"\n%s", out)
	}
	if !strings.Contains(string(out), `"uuid": "aaaabbbb-cccc-dddd-eeee-ffffffffffff"`) {
		t.Errorf("rendered JSON should contain the uuid\n%s", out)
	}
	if !strings.Contains(string(out), `"congestion_control": "bbr"`) {
		t.Errorf("rendered JSON should contain congestion_control\n%s", out)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Strategy rendering
// ---------------------------------------------------------------------------

func twoNodeCfg(s singbox.Strategy) singbox.Config {
	return singbox.Config{
		SOCKSListenAddr: "127.0.0.1",
		SOCKSListenPort: 1080,
		Strategy:        s,
		Outbounds: []singbox.Outbound{
			{Type: singbox.OutboundVLESSReality, Tag: "node-a", Server: "1.1.1.1", Port: 443,
				UUID: "aaaaaaaa-0000-0000-0000-000000000001", TLSServer: "a.example.com",
				PublicKey: "AAAA=", ShortID: "aa"},
			{Type: singbox.OutboundVLESSReality, Tag: "node-b", Server: "2.2.2.2", Port: 443,
				UUID: "bbbbbbbb-0000-0000-0000-000000000002", TLSServer: "b.example.com",
				PublicKey: "BBBB=", ShortID: "bb"},
		},
	}
}

func proxyGroup(out []byte) map[string]any {
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil
	}
	for _, raw := range doc["outbounds"].([]any) {
		ob := raw.(map[string]any)
		if ob["tag"] == "proxy" {
			return ob
		}
	}
	return nil
}

func TestStrategyURLTestRendersURLTest(t *testing.T) {
	out, err := twoNodeCfg(singbox.StrategyURLTest).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	g := proxyGroup(out)
	if g == nil {
		t.Fatal("proxy group not found")
	}
	if g["type"] != "urltest" {
		t.Errorf("want type=urltest, got %v", g["type"])
	}
	if _, ok := g["url"]; !ok {
		t.Error("urltest group must have url field")
	}
}

func TestStrategyFallbackRendersURLTest(t *testing.T) {
	out, err := twoNodeCfg(singbox.StrategyFallback).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	g := proxyGroup(out)
	if g["type"] != "urltest" {
		t.Errorf("fallback strategy: want type=urltest, got %v", g["type"])
	}
	if g["interrupt_exist_connections"] != true {
		t.Errorf("fallback strategy should interrupt existing connections on health changes, got %v", g["interrupt_exist_connections"])
	}
}

func TestStrategySelectorRendersSelector(t *testing.T) {
	out, err := twoNodeCfg(singbox.StrategySelector).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	g := proxyGroup(out)
	if g["type"] != "selector" {
		t.Errorf("selector strategy: want type=selector, got %v", g["type"])
	}
	if g["default"] != "node-a" {
		t.Errorf("selector default should be first outbound tag, got %v", g["default"])
	}
}

func TestStrategyRoundRobinRendersSelector(t *testing.T) {
	out, err := twoNodeCfg(singbox.StrategyRoundRobin).Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	g := proxyGroup(out)
	if g["type"] != "selector" {
		t.Errorf("roundrobin strategy: want type=selector (no native round-robin in sing-box), got %v", g["type"])
	}
}

// ---------------------------------------------------------------------------
// Strategy validation
// ---------------------------------------------------------------------------

func TestValidateURLTestRequiresOneOutbound(t *testing.T) {
	cfg := singbox.Config{
		SOCKSListenAddr: "127.0.0.1", SOCKSListenPort: 1080,
		Strategy:  singbox.StrategyURLTest,
		Outbounds: []singbox.Outbound{},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty outbounds")
	}
}

func TestValidateRoundRobinRequiresTwoOutbounds(t *testing.T) {
	one := singbox.Config{
		SOCKSListenAddr: "127.0.0.1", SOCKSListenPort: 1080,
		Strategy: singbox.StrategyRoundRobin,
		Outbounds: []singbox.Outbound{
			{Tag: "only", Type: singbox.OutboundVLESSReality},
		},
	}
	if err := one.Validate(); err == nil {
		t.Error("expected error: round-robin needs >= 2 outbounds")
	}
	two := twoNodeCfg(singbox.StrategyRoundRobin)
	if err := two.Validate(); err != nil {
		t.Errorf("two outbounds should satisfy round-robin: %v", err)
	}
}

func TestRenderFailsWhenValidationFails(t *testing.T) {
	cfg := singbox.Config{
		SOCKSListenAddr: "127.0.0.1", SOCKSListenPort: 1080,
		Strategy:  singbox.StrategyURLTest,
		Outbounds: []singbox.Outbound{},
	}
	_, err := cfg.Render()
	if err == nil {
		t.Error("Render should fail when Validate fails")
	}
}

// ---------------------------------------------------------------------------
// Golden files per strategy
// ---------------------------------------------------------------------------

const updateStrategyGoldens = false

func TestStrategyGolden(t *testing.T) {
	cases := []struct {
		name     string
		strategy singbox.Strategy
		golden   string
	}{
		{"urltest", singbox.StrategyURLTest, "testdata/strategy_urltest.json"},
		{"fallback", singbox.StrategyFallback, "testdata/strategy_fallback.json"},
		{"selector", singbox.StrategySelector, "testdata/strategy_selector.json"},
		{"roundrobin", singbox.StrategyRoundRobin, "testdata/strategy_roundrobin.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := twoNodeCfg(tc.strategy)
			out, err := cfg.Render()
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if updateStrategyGoldens {
				os.MkdirAll("testdata", 0o755)      //nolint:errcheck
				os.WriteFile(tc.golden, out, 0o644) //nolint:errcheck
				t.Logf("updated %s", tc.golden)
				return
			}
			want, err := os.ReadFile(tc.golden)
			if err != nil {
				t.Fatalf("read golden %s: %v (set updateStrategyGoldens=true to generate)", tc.golden, err)
			}
			if string(out) != string(want) {
				t.Errorf("%s strategy golden mismatch:\n--- want ---\n%s\n--- got ---\n%s", tc.name, want, out)
			}
		})
	}
}
