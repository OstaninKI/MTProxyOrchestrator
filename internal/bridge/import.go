package bridge

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ImportVLESS parses a vless:// share URL and returns a Node.
//
// Expected format (VLESS Reality):
//
//	vless://UUID@host:port?security=reality&sni=SNI&pbk=PUBKEY&sid=SHORTID[&flow=FLOW][&type=tcp]#TAG
//
// The fragment (after #) becomes the node Tag; if absent, host:port is used.
func ImportVLESS(rawURL string) (Node, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Node{}, fmt.Errorf("import vless: parse url: %w", err)
	}
	if strings.ToLower(u.Scheme) != "vless" {
		return Node{}, fmt.Errorf("import vless: expected vless:// scheme, got %q", u.Scheme)
	}

	uuid := u.User.Username()
	if uuid == "" {
		return Node{}, errors.New("import vless: uuid is missing from URL userinfo")
	}

	host := u.Hostname()
	if host == "" {
		return Node{}, errors.New("import vless: host is missing")
	}

	portStr := u.Port()
	if portStr == "" {
		return Node{}, errors.New("import vless: port is missing")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return Node{}, fmt.Errorf("import vless: invalid port %q", portStr)
	}

	q := u.Query()

	security := strings.ToLower(q.Get("security"))
	if security != "reality" {
		return Node{}, fmt.Errorf("import vless: security=%q; only reality is supported", security)
	}

	sni := q.Get("sni")
	if sni == "" {
		return Node{}, errors.New("import vless: sni is required for reality")
	}

	pbk := q.Get("pbk")
	if pbk == "" {
		return Node{}, errors.New("import vless: pbk (public key) is required for reality")
	}

	sid := q.Get("sid") // short_id — may be empty string, but key must be present
	if !q.Has("sid") {
		return Node{}, errors.New("import vless: sid (short_id) parameter is required for reality")
	}

	flow := q.Get("flow")

	// uTLS fingerprint (fp=) is required by the sing-box Reality client.
	// Default to "chrome" when the share URL omits it.
	fp := q.Get("fp")
	if fp == "" {
		fp = "chrome"
	}

	tag := u.Fragment
	if tag == "" {
		tag = fmt.Sprintf("%s:%d", host, port)
	} else {
		// URL-decode fragment (browsers encode it)
		tag, _ = url.QueryUnescape(tag)
	}

	n := Node{
		Type:        NodeTypeVLESSReality,
		Tag:         tag,
		Host:        host,
		Port:        port,
		UUID:        uuid,
		Flow:        flow,
		SNI:         sni,
		PublicKey:   pbk,
		ShortID:     sid,
		Fingerprint: fp,
		Enabled:     true,
	}

	if err := n.Validate(); err != nil {
		return Node{}, fmt.Errorf("import vless: %w", err)
	}

	return n, nil
}

// ImportTrojan parses a trojan:// share URL and returns a Node.
//
// Expected format:
//
//	trojan://password@host:port?sni=SNI[&security=tls][&type=tcp]#tag
func ImportTrojan(rawURL string) (Node, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Node{}, fmt.Errorf("import trojan: parse url: %w", err)
	}
	if strings.ToLower(u.Scheme) != "trojan" {
		return Node{}, fmt.Errorf("import trojan: expected trojan:// scheme, got %q", u.Scheme)
	}

	password := u.User.Username()
	if password == "" {
		return Node{}, errors.New("import trojan: password is missing from URL userinfo")
	}

	host := u.Hostname()
	if host == "" {
		return Node{}, errors.New("import trojan: host is missing")
	}

	portStr := u.Port()
	if portStr == "" {
		return Node{}, errors.New("import trojan: port is missing")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return Node{}, fmt.Errorf("import trojan: invalid port %q", portStr)
	}

	q := u.Query()
	sni := q.Get("sni")
	if sni == "" {
		return Node{}, errors.New("import trojan: sni is required")
	}

	tag := u.Fragment
	if tag == "" {
		tag = fmt.Sprintf("%s:%d", host, port)
	} else {
		tag, _ = url.QueryUnescape(tag)
	}

	return Node{
		Type:     NodeTypeTrojan,
		Tag:      tag,
		Host:     host,
		Port:     port,
		SNI:      sni,
		Password: password,
		Enabled:  true,
	}, nil
}

// ImportShadowsocks parses an ss:// share URL and returns a Node.
//
// Supports two formats:
//   - ss://base64(method:password)@host:port#tag  (SIP002)
//   - ss://base64(method:password@host:port)#tag  (legacy)
func ImportShadowsocks(rawURL string) (Node, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Node{}, fmt.Errorf("import ss: parse url: %w", err)
	}
	if strings.ToLower(u.Scheme) != "ss" {
		return Node{}, fmt.Errorf("import ss: expected ss:// scheme, got %q", u.Scheme)
	}

	var method, password, host string
	var port int

	// Try SIP002 format: userinfo is base64(method:password), host:port in URL authority.
	if u.Hostname() != "" {
		// SIP002: userinfo = base64(method:password)
		userinfo := u.User.Username()
		decoded, decErr := base64DecodeLoose(userinfo)
		if decErr != nil {
			return Node{}, fmt.Errorf("import ss: decode userinfo: %w", decErr)
		}
		parts := strings.SplitN(decoded, ":", 2)
		if len(parts) != 2 {
			return Node{}, errors.New("import ss: userinfo must be base64(method:password)")
		}
		method = parts[0]
		password = parts[1]
		host = u.Hostname()
		portStr := u.Port()
		if portStr == "" {
			return Node{}, errors.New("import ss: port is missing")
		}
		p, pErr := strconv.Atoi(portStr)
		if pErr != nil || p < 1 || p > 65535 {
			return Node{}, fmt.Errorf("import ss: invalid port %q", portStr)
		}
		port = p
	} else {
		// Legacy format: entire authority is base64(method:password@host:port)
		userinfo := u.User.Username()
		decoded, decErr := base64DecodeLoose(userinfo)
		if decErr != nil {
			return Node{}, fmt.Errorf("import ss: decode legacy userinfo: %w", decErr)
		}
		// decoded = method:password@host:port
		atIdx := strings.LastIndex(decoded, "@")
		if atIdx < 0 {
			return Node{}, errors.New("import ss: legacy format missing '@' after base64 decode")
		}
		methodPass := decoded[:atIdx]
		hostPort := decoded[atIdx+1:]
		parts := strings.SplitN(methodPass, ":", 2)
		if len(parts) != 2 {
			return Node{}, errors.New("import ss: legacy format missing ':' between method and password")
		}
		method = parts[0]
		password = parts[1]
		colonIdx := strings.LastIndex(hostPort, ":")
		if colonIdx < 0 {
			return Node{}, errors.New("import ss: legacy format missing port")
		}
		host = hostPort[:colonIdx]
		portStr := hostPort[colonIdx+1:]
		p, pErr := strconv.Atoi(portStr)
		if pErr != nil || p < 1 || p > 65535 {
			return Node{}, fmt.Errorf("import ss: invalid port %q", portStr)
		}
		port = p
	}

	if host == "" {
		return Node{}, errors.New("import ss: host is missing")
	}
	if method == "" {
		return Node{}, errors.New("import ss: method is missing")
	}
	if password == "" {
		return Node{}, errors.New("import ss: password is missing")
	}

	tag := u.Fragment
	if tag == "" {
		tag = fmt.Sprintf("%s:%d", host, port)
	} else {
		tag, _ = url.QueryUnescape(tag)
	}

	return Node{
		Type:     NodeTypeShadowsocks,
		Tag:      tag,
		Host:     host,
		Port:     port,
		Method:   method,
		Password: password,
		Enabled:  true,
	}, nil
}

// base64DecodeLoose decodes standard or URL-safe base64, with or without padding.
func base64DecodeLoose(s string) (string, error) {
	// Normalise to standard base64 with padding.
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ImportHysteria2 parses a hysteria2:// or hy2:// share URL and returns a Node.
//
// Expected format:
//
//	hysteria2://password@host:port?sni=SNI#tag
func ImportHysteria2(rawURL string) (Node, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Node{}, fmt.Errorf("import hysteria2: parse url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "hysteria2" && scheme != "hy2" {
		return Node{}, fmt.Errorf("import hysteria2: expected hysteria2:// or hy2:// scheme, got %q", u.Scheme)
	}

	password := u.User.Username()
	if password == "" {
		return Node{}, errors.New("import hysteria2: password is missing from URL userinfo")
	}

	host := u.Hostname()
	if host == "" {
		return Node{}, errors.New("import hysteria2: host is missing")
	}

	portStr := u.Port()
	if portStr == "" {
		return Node{}, errors.New("import hysteria2: port is missing")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return Node{}, fmt.Errorf("import hysteria2: invalid port %q", portStr)
	}

	q := u.Query()
	sni := q.Get("sni")
	if sni == "" {
		return Node{}, errors.New("import hysteria2: sni is required")
	}

	tag := u.Fragment
	if tag == "" {
		tag = fmt.Sprintf("%s:%d", host, port)
	} else {
		tag, _ = url.QueryUnescape(tag)
	}

	return Node{
		Type:     NodeTypeHysteria2,
		Tag:      tag,
		Host:     host,
		Port:     port,
		SNI:      sni,
		Password: password,
		Enabled:  true,
	}, nil
}

// ImportTUIC parses a tuic:// share URL and returns a Node.
//
// Expected format:
//
//	tuic://uuid:password@host:port?sni=SNI[&congestion_control=bbr]#tag
func ImportTUIC(rawURL string) (Node, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Node{}, fmt.Errorf("import tuic: parse url: %w", err)
	}
	if strings.ToLower(u.Scheme) != "tuic" {
		return Node{}, fmt.Errorf("import tuic: expected tuic:// scheme, got %q", u.Scheme)
	}

	uuid := u.User.Username()
	if uuid == "" {
		return Node{}, errors.New("import tuic: uuid is missing from URL userinfo")
	}

	password, _ := u.User.Password()
	if password == "" {
		return Node{}, errors.New("import tuic: password is missing from URL userinfo")
	}

	host := u.Hostname()
	if host == "" {
		return Node{}, errors.New("import tuic: host is missing")
	}

	portStr := u.Port()
	if portStr == "" {
		return Node{}, errors.New("import tuic: port is missing")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return Node{}, fmt.Errorf("import tuic: invalid port %q", portStr)
	}

	q := u.Query()
	sni := q.Get("sni")
	if sni == "" {
		return Node{}, errors.New("import tuic: sni is required")
	}

	congestionControl := q.Get("congestion_control")
	if congestionControl == "" {
		congestionControl = "bbr"
	}

	tag := u.Fragment
	if tag == "" {
		tag = fmt.Sprintf("%s:%d", host, port)
	} else {
		tag, _ = url.QueryUnescape(tag)
	}

	return Node{
		Type:              NodeTypeTUIC,
		Tag:               tag,
		Host:              host,
		Port:              port,
		SNI:               sni,
		UUID:              uuid,
		Password:          password,
		CongestionControl: congestionControl,
		Enabled:           true,
	}, nil
}

// Import parses any supported share URL and returns a Node.
// Supported schemes: vless, trojan, ss, hysteria2, hy2, tuic.
func Import(rawURL string) (Node, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Node{}, fmt.Errorf("import: parse url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "vless":
		return ImportVLESS(rawURL)
	case "trojan":
		return ImportTrojan(rawURL)
	case "ss":
		return ImportShadowsocks(rawURL)
	case "hysteria2", "hy2":
		return ImportHysteria2(rawURL)
	case "tuic":
		return ImportTUIC(rawURL)
	default:
		return Node{}, fmt.Errorf("import: unsupported scheme %q", u.Scheme)
	}
}
