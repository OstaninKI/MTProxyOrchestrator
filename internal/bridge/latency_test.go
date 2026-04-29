package bridge_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
)

func makeNode(id int64, tag string, enabled bool) bridge.Node {
	return bridge.Node{
		ID:      id,
		Tag:     tag,
		Type:    bridge.NodeTypeVLESSReality,
		Host:    "example.com",
		Port:    443,
		Enabled: enabled,
	}
}

func fakeTester(dialFn bridge.Dialer) bridge.Tester {
	return bridge.Tester{
		SOCKSAddr: "127.0.0.1:1080",
		Dial:      dialFn,
		Now:       func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
}

func TestProbeAllSkipsDisabled(t *testing.T) {
	nl := bridge.NodeList{
		Nodes: []bridge.Node{
			{ID: 1, Tag: "enabled-node", Type: bridge.NodeTypeVLESSReality, Host: "enabled.example.com", Port: 443, Enabled: true},
			{ID: 2, Tag: "disabled-node", Type: bridge.NodeTypeVLESSReality, Host: "disabled.example.com", Port: 8443, Enabled: false},
		},
	}
	calledTags := map[string]bool{}
	tester := fakeTester(func(socksAddr, target string) (time.Duration, error) {
		calledTags[target] = true
		return 10 * time.Millisecond, nil
	})

	results := tester.ProbeAll(nl)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Tag != "enabled-node" {
		t.Errorf("expected enabled-node, got %s", results[0].Tag)
	}
	if calledTags["disabled.example.com:8443"] {
		t.Error("disabled node should not have been probed")
	}
}

func TestProbeAllAllEnabled(t *testing.T) {
	nl := bridge.NodeList{
		Nodes: []bridge.Node{
			makeNode(1, "node-a", true),
			makeNode(2, "node-b", true),
			makeNode(3, "node-c", true),
		},
	}
	tester := fakeTester(func(socksAddr, target string) (time.Duration, error) {
		return 5 * time.Millisecond, nil
	})

	results := tester.ProbeAll(nl)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("node %s: unexpected error: %v", r.Tag, r.Err)
		}
	}
}

func TestProbeAllRecordsErrors(t *testing.T) {
	nl := bridge.NodeList{
		Nodes: []bridge.Node{
			makeNode(1, "bad-node", true),
		},
	}
	dialErr := errors.New("connection refused")
	tester := fakeTester(func(socksAddr, target string) (time.Duration, error) {
		return 0, dialErr
	})

	results := tester.ProbeAll(nl)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Error("expected error in ProbeResult, got nil")
	}
	if results[0].Latency != 0 {
		t.Errorf("expected zero latency on error, got %v", results[0].Latency)
	}
}

func TestUpdateLatenciesPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbounds.json")

	nl := bridge.NodeList{
		Nodes: []bridge.Node{
			makeNode(1, "node-a", true),
		},
	}
	if err := nl.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	fixedNow := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	tester := bridge.Tester{
		SOCKSAddr: "127.0.0.1:1080",
		Dial: func(socksAddr, target string) (time.Duration, error) {
			return 42 * time.Millisecond, nil
		},
		Now: func() time.Time { return fixedNow },
	}

	results := []bridge.ProbeResult{
		{NodeID: 1, Tag: "node-a", Latency: 42 * time.Millisecond},
	}
	if err := tester.UpdateLatencies(&nl, results, path); err != nil {
		t.Fatalf("UpdateLatencies: %v", err)
	}

	loaded, err := bridge.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(loaded.Nodes))
	}
	n := loaded.Nodes[0]
	if n.LastLatency != 42 {
		t.Errorf("expected LastLatency=42, got %d", n.LastLatency)
	}
	if n.LastChecked == nil {
		t.Fatal("expected LastChecked to be set")
	}
	if !n.LastChecked.Equal(fixedNow) {
		t.Errorf("expected LastChecked=%v, got %v", fixedNow, *n.LastChecked)
	}
}

func TestUpdateLatenciesLeavesUnprobed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbounds.json")

	oldChecked := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	nl := bridge.NodeList{
		Nodes: []bridge.Node{
			{
				ID: 1, Tag: "probed", Type: bridge.NodeTypeVLESSReality,
				Host: "a.com", Port: 443, Enabled: true,
				LastLatency: 0,
			},
			{
				ID: 2, Tag: "unprobed", Type: bridge.NodeTypeVLESSReality,
				Host: "b.com", Port: 443, Enabled: true,
				LastLatency: 99, LastChecked: &oldChecked,
			},
		},
	}
	if err := nl.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	tester := fakeTester(nil)
	results := []bridge.ProbeResult{
		{NodeID: 1, Tag: "probed", Latency: 10 * time.Millisecond},
	}
	if err := tester.UpdateLatencies(&nl, results, path); err != nil {
		t.Fatalf("UpdateLatencies: %v", err)
	}

	loaded, err := bridge.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var unprobedNode bridge.Node
	for _, n := range loaded.Nodes {
		if n.ID == 2 {
			unprobedNode = n
		}
	}
	if unprobedNode.LastLatency != 99 {
		t.Errorf("unprobed node latency changed: got %d, want 99", unprobedNode.LastLatency)
	}
	if unprobedNode.LastChecked == nil || !unprobedNode.LastChecked.Equal(oldChecked) {
		t.Errorf("unprobed node LastChecked changed unexpectedly")
	}

	// Make sure we can also verify the file on disk didn't corrupt
	if _, err := os.Stat(path); err != nil {
		t.Errorf("saved file not found: %v", err)
	}
}

func TestProbeAllReturnsLatency(t *testing.T) {
	nl := bridge.NodeList{
		Nodes: []bridge.Node{
			makeNode(1, "fast-node", true),
		},
	}
	tester := fakeTester(func(socksAddr, target string) (time.Duration, error) {
		return 42 * time.Millisecond, nil
	})

	results := tester.ProbeAll(nl)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Latency != 42*time.Millisecond {
		t.Errorf("expected 42ms latency, got %v", results[0].Latency)
	}
	if results[0].Err != nil {
		t.Errorf("unexpected error: %v", results[0].Err)
	}
}
