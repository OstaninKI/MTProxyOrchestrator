package singbox_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/singbox"
)

const updateGolden = false

var testNode = singbox.VLESSOutbound{
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
		Outbounds:       []singbox.VLESSOutbound{testNode},
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
		Outbounds: []singbox.VLESSOutbound{{
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
