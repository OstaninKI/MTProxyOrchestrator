package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// NodeType identifies the outbound protocol.
type NodeType string

const (
	NodeTypeVLESSReality NodeType = "vless-reality"
	NodeTypeTrojan       NodeType = "trojan"
	NodeTypeShadowsocks  NodeType = "shadowsocks"
	NodeTypeHysteria2    NodeType = "hysteria2"
	NodeTypeTUIC         NodeType = "tuic"
)

// Node represents a single outbound proxy node.
type Node struct {
	ID        int64    `json:"id"`
	Type      NodeType `json:"type"`
	Tag       string   `json:"tag"`
	Host      string   `json:"host"`
	Port      int      `json:"port"`
	UUID      string   `json:"uuid,omitempty"`
	Flow      string   `json:"flow,omitempty"`
	SNI       string   `json:"sni,omitempty"`
	PublicKey string   `json:"public_key,omitempty"`
	ShortID   string   `json:"short_id,omitempty"`
	// Multi-protocol fields
	Password          string     `json:"password,omitempty"`           // Trojan, SS, Hysteria2, TUIC
	Method            string     `json:"method,omitempty"`             // Shadowsocks cipher method
	CongestionControl string     `json:"congestion_control,omitempty"` // TUIC
	Enabled           bool       `json:"enabled"`
	LastLatency       int64      `json:"last_latency_ms,omitempty"` // milliseconds; 0 = not measured
	LastChecked       *time.Time `json:"last_checked,omitempty"`
}

// Validate returns an error if required VLESS Reality fields are missing.
func (n Node) Validate() error {
	var errs []error
	if n.Tag == "" {
		errs = append(errs, errors.New("tag is required"))
	}
	if n.Host == "" {
		errs = append(errs, errors.New("host is required"))
	}
	if n.Port < 1 || n.Port > 65535 {
		errs = append(errs, fmt.Errorf("port %d is out of range", n.Port))
	}
	if n.UUID == "" {
		errs = append(errs, errors.New("uuid is required"))
	}
	if n.SNI == "" {
		errs = append(errs, errors.New("sni is required"))
	}
	if n.PublicKey == "" {
		errs = append(errs, errors.New("public_key is required"))
	}
	// short_id may be empty string (valid for Reality), but must be present
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// NodeList is the root structure of outbounds.json.
type NodeList struct {
	Nodes []Node `json:"nodes"`
}

// Load reads a NodeList from path. Returns empty list if file does not exist.
func Load(path string) (NodeList, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NodeList{}, nil
	}
	if err != nil {
		return NodeList{}, fmt.Errorf("bridge: read %s: %w", path, err)
	}
	var nl NodeList
	if err := json.Unmarshal(data, &nl); err != nil {
		return NodeList{}, fmt.Errorf("bridge: parse %s: %w", path, err)
	}
	return nl, nil
}

// Save writes the NodeList to path atomically (write tmp + rename).
func (l NodeList) Save(path string) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("bridge: marshal nodes: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("bridge: mkdir %s: %w", dir, err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("bridge: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("bridge: rename to %s: %w", path, err)
	}
	return nil
}

// Active returns only enabled, non-zero nodes suitable for sing-box config.
func (l NodeList) Active() []Node {
	var out []Node
	for _, n := range l.Nodes {
		if n.Enabled {
			out = append(out, n)
		}
	}
	return out
}

// NextID returns the next available ID (max existing ID + 1, starting at 1).
func (l NodeList) NextID() int64 {
	var max int64
	for _, n := range l.Nodes {
		if n.ID > max {
			max = n.ID
		}
	}
	return max + 1
}
