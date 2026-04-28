package bridge

import (
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

	tag := u.Fragment
	if tag == "" {
		tag = fmt.Sprintf("%s:%d", host, port)
	} else {
		// URL-decode fragment (browsers encode it)
		tag, _ = url.QueryUnescape(tag)
	}

	n := Node{
		Type:      NodeTypeVLESSReality,
		Tag:       tag,
		Host:      host,
		Port:      port,
		UUID:      uuid,
		Flow:      flow,
		SNI:       sni,
		PublicKey: pbk,
		ShortID:   sid,
		Enabled:   true,
	}

	if err := n.Validate(); err != nil {
		return Node{}, fmt.Errorf("import vless: %w", err)
	}

	return n, nil
}
