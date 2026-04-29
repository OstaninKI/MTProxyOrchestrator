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

// OutboundType identifies the outbound protocol for sing-box config rendering.
type OutboundType string

const (
	OutboundVLESSReality OutboundType = "vless-reality"
	OutboundTrojan       OutboundType = "trojan"
	OutboundShadowsocks  OutboundType = "shadowsocks"
	OutboundHysteria2    OutboundType = "hysteria2"
	OutboundTUIC         OutboundType = "tuic"
)

// Outbound is a protocol-agnostic outbound node for sing-box config rendering.
type Outbound struct {
	Type   OutboundType
	Tag    string
	Server string
	Port   int
	// VLESS Reality fields
	UUID      string
	Flow      string
	TLSServer string // SNI
	PublicKey string
	ShortID   string
	// Multi-protocol fields
	Password          string // Trojan, Hysteria2, TUIC, SS
	Method            string // Shadowsocks cipher
	CongestionControl string // TUIC, default "bbr"
}

// VLESSOutbound describes a VLESS + Reality outbound node.
// Kept for backward compatibility; callers should migrate to Outbound.
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

// toOutbound converts a VLESSOutbound to the generic Outbound type.
func (v VLESSOutbound) toOutbound() Outbound {
	return Outbound{
		Type:      OutboundVLESSReality,
		Tag:       v.Tag,
		Server:    v.Server,
		Port:      v.Port,
		UUID:      v.UUID,
		Flow:      v.Flow,
		TLSServer: v.TLSServer,
		PublicKey: v.PublicKey,
		ShortID:   v.ShortID,
	}
}

// Config holds parameters to render a sing-box JSON configuration.
type Config struct {
	SOCKSListenAddr string
	SOCKSListenPort int
	Strategy        Strategy
	Outbounds       []Outbound
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
		ob := buildOutbound(o)
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

// buildOutbound renders a single Outbound to its sing-box JSON map.
func buildOutbound(o Outbound) map[string]any {
	switch o.Type {
	case OutboundTrojan:
		return map[string]any{
			"type":        "trojan",
			"tag":         o.Tag,
			"server":      o.Server,
			"server_port": o.Port,
			"password":    o.Password,
			"tls": map[string]any{
				"enabled":     true,
				"server_name": o.TLSServer,
			},
		}
	case OutboundShadowsocks:
		return map[string]any{
			"type":        "shadowsocks",
			"tag":         o.Tag,
			"server":      o.Server,
			"server_port": o.Port,
			"method":      o.Method,
			"password":    o.Password,
		}
	case OutboundHysteria2:
		return map[string]any{
			"type":        "hysteria2",
			"tag":         o.Tag,
			"server":      o.Server,
			"server_port": o.Port,
			"password":    o.Password,
			"tls": map[string]any{
				"enabled":     true,
				"server_name": o.TLSServer,
			},
		}
	case OutboundTUIC:
		cc := o.CongestionControl
		if cc == "" {
			cc = "bbr"
		}
		return map[string]any{
			"type":               "tuic",
			"tag":                o.Tag,
			"server":             o.Server,
			"server_port":        o.Port,
			"uuid":               o.UUID,
			"password":           o.Password,
			"congestion_control": cc,
			"tls": map[string]any{
				"enabled":     true,
				"server_name": o.TLSServer,
			},
		}
	default:
		// OutboundVLESSReality and unknown types fall through to VLESS rendering.
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
		return ob
	}
}
