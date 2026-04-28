package singbox

import (
	"encoding/json"
	"fmt"
)

// Strategy is the sing-box outbound group selection strategy.
type Strategy string

const (
	StrategyURLTest    Strategy = "urltest"
	StrategyFallback   Strategy = "fallback"
	StrategyRoundRobin Strategy = "roundrobin"
	StrategySelector   Strategy = "selector"
)

// VLESSOutbound describes a VLESS + Reality outbound node.
type VLESSOutbound struct {
	Tag       string
	Server    string
	Port      int
	UUID      string
	Flow      string // e.g. "xtls-rprx-vision", may be empty
	TLSServer string // SNI for Reality
	PublicKey string // Reality server public key (base64)
	ShortID   string // Reality short ID (hex)
}

// Config holds parameters to render a sing-box JSON configuration.
type Config struct {
	SOCKSListenAddr string
	SOCKSListenPort int
	Strategy        Strategy
	Outbounds       []VLESSOutbound
}

// Render produces a deterministic, indented sing-box JSON configuration.
func (c Config) Render() ([]byte, error) {
	doc := c.build()
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("singbox render: %w", err)
	}
	return append(b, '\n'), nil
}

// build constructs the sing-box config as nested maps for JSON marshalling.
func (c Config) build() map[string]any {
	outboundTags := make([]string, len(c.Outbounds))
	outbounds := make([]map[string]any, 0, len(c.Outbounds)+2)

	for i, o := range c.Outbounds {
		outboundTags[i] = o.Tag
		ob := map[string]any{
			"type":        "vless",
			"tag":         o.Tag,
			"server":      o.Server,
			"server_port": o.Port,
			"uuid":        o.UUID,
			"tls": map[string]any{
				"enabled":     true,
				"server_name": o.TLSServer,
				"reality": map[string]any{
					"enabled":    true,
					"public_key": o.PublicKey,
					"short_id":   o.ShortID,
				},
			},
		}
		if o.Flow != "" {
			ob["flow"] = o.Flow
		}
		outbounds = append(outbounds, ob)
	}

	outbounds = append(outbounds, map[string]any{
		"type":      string(c.Strategy),
		"tag":       "proxy",
		"outbounds": outboundTags,
	})

	outbounds = append(outbounds, map[string]any{
		"type": "direct",
		"tag":  "direct",
	})

	return map[string]any{
		"inbounds": []map[string]any{
			{
				"type":        "socks",
				"tag":         "socks-in",
				"listen":      c.SOCKSListenAddr,
				"listen_port": c.SOCKSListenPort,
			},
		},
		"outbounds": outbounds,
		"route": map[string]any{
			"final": "proxy",
		},
	}
}
