package bridge_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/bridge"
)

func validNode() bridge.Node {
	return bridge.Node{
		ID:        1,
		Type:      bridge.NodeTypeVLESSReality,
		Tag:       "vless-node-1",
		Host:      "1.2.3.4",
		Port:      443,
		UUID:      "a3b4c5d6-e7f8-4a2b-9c0d-1e2f3a4b5c6d",
		Flow:      "xtls-rprx-vision",
		SNI:       "example.com",
		PublicKey: "ABC123publickey==",
		ShortID:   "abcd1234",
		Enabled:   true,
	}
}

func TestNodeValidateOK(t *testing.T) {
	n := validNode()
	if err := n.Validate(); err != nil {
		t.Fatalf("valid node failed validation: %v", err)
	}
}

func TestNodeValidateMissingTag(t *testing.T) {
	n := validNode()
	n.Tag = ""
	if err := n.Validate(); err == nil {
		t.Error("expected error for missing tag")
	}
}

func TestNodeValidateMissingHost(t *testing.T) {
	n := validNode()
	n.Host = ""
	if err := n.Validate(); err == nil {
		t.Error("expected error for missing host")
	}
}

func TestNodeValidateInvalidPort(t *testing.T) {
	n := validNode()
	n.Port = 0
	if err := n.Validate(); err == nil {
		t.Error("expected error for port 0")
	}
	n.Port = 70000
	if err := n.Validate(); err == nil {
		t.Error("expected error for port 70000")
	}
}

func TestNodeValidateMissingUUID(t *testing.T) {
	n := validNode()
	n.UUID = ""
	if err := n.Validate(); err == nil {
		t.Error("expected error for missing uuid")
	}
}

func TestNodeValidateMissingSNI(t *testing.T) {
	n := validNode()
	n.SNI = ""
	if err := n.Validate(); err == nil {
		t.Error("expected error for missing sni")
	}
}

func TestNodeValidateMissingPublicKey(t *testing.T) {
	n := validNode()
	n.PublicKey = ""
	if err := n.Validate(); err == nil {
		t.Error("expected error for missing public_key")
	}
}

func TestNodeValidateEmptyShortIDAllowed(t *testing.T) {
	n := validNode()
	n.ShortID = ""
	if err := n.Validate(); err != nil {
		t.Fatalf("empty short_id should be allowed: %v", err)
	}
}

func TestNodeLoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes", "outbounds.json")

	now := time.Now().UTC().Truncate(time.Second)
	nl := bridge.NodeList{
		Nodes: []bridge.Node{
			{
				ID: 1, Type: bridge.NodeTypeVLESSReality,
				Tag: "n1", Host: "10.0.0.1", Port: 443,
				UUID: "uuid-1", SNI: "sni.example.com",
				PublicKey: "pk1", ShortID: "sid1",
				Enabled: true, LastLatency: 42, LastChecked: &now,
			},
		},
	}

	if err := nl.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := bridge.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(loaded.Nodes))
	}
	got := loaded.Nodes[0]
	if got.Tag != "n1" || got.Host != "10.0.0.1" || got.Port != 443 {
		t.Errorf("unexpected node fields: %+v", got)
	}
	if got.LastLatency != 42 {
		t.Errorf("want last_latency_ms=42, got %d", got.LastLatency)
	}
}

func TestNodeLoadMissingFile(t *testing.T) {
	nl, err := bridge.Load("/tmp/definitely-does-not-exist-outbounds.json")
	if err != nil {
		t.Fatalf("expected empty list for missing file, got error: %v", err)
	}
	if len(nl.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nl.Nodes))
	}
}

func TestNodeListActive(t *testing.T) {
	nl := bridge.NodeList{
		Nodes: []bridge.Node{
			{ID: 1, Tag: "a", Enabled: true},
			{ID: 2, Tag: "b", Enabled: false},
			{ID: 3, Tag: "c", Enabled: true},
		},
	}
	active := nl.Active()
	if len(active) != 2 {
		t.Fatalf("want 2 active nodes, got %d", len(active))
	}
	for _, n := range active {
		if !n.Enabled {
			t.Errorf("Active() returned disabled node %s", n.Tag)
		}
	}
}

func TestNodeListNextID(t *testing.T) {
	empty := bridge.NodeList{}
	if id := empty.NextID(); id != 1 {
		t.Errorf("empty list: want NextID=1, got %d", id)
	}

	nl := bridge.NodeList{
		Nodes: []bridge.Node{{ID: 3}, {ID: 7}, {ID: 2}},
	}
	if id := nl.NextID(); id != 8 {
		t.Errorf("want NextID=8, got %d", id)
	}
}

func TestSaveCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "outbounds.json")
	nl := bridge.NodeList{Nodes: []bridge.Node{}}
	if err := nl.Save(path); err != nil {
		t.Fatalf("Save with deep path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}
