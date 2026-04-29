package bridge

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"time"
)

// Dialer is a function that establishes a TCP connection through a SOCKS5 proxy.
// socksAddr is "host:port" of the SOCKS5 server; target is "host:port" to reach.
// Returns the round-trip duration and any error.
// Injectable for tests.
type Dialer func(socksAddr, target string) (time.Duration, error)

// Tester runs latency probes for each node in a NodeList through a SOCKS5 proxy.
type Tester struct {
	SOCKSAddr string // e.g. "127.0.0.1:1080"
	Dial      Dialer
	Now       func() time.Time // injectable clock
}

// ProbeResult holds the outcome of probing a single node.
type ProbeResult struct {
	NodeID  int64
	Tag     string
	Latency time.Duration // 0 if error
	Err     error
}

// ProbeAll tests every enabled node in nl through the SOCKS5 proxy.
// Returns one ProbeResult per enabled node.
// Does NOT modify nl — call UpdateLatencies to persist results.
func (t Tester) ProbeAll(nl NodeList) []ProbeResult {
	var results []ProbeResult
	for _, n := range nl.Nodes {
		if !n.Enabled {
			continue
		}
		target := fmt.Sprintf("%s:%d", n.Host, n.Port)
		latency, err := t.Dial(t.SOCKSAddr, target)
		r := ProbeResult{
			NodeID: n.ID,
			Tag:    n.Tag,
			Err:    err,
		}
		if err == nil {
			r.Latency = latency
		}
		results = append(results, r)
	}
	return results
}

// UpdateLatencies updates LastLatency and LastChecked for each node in nl
// based on results, and saves the updated list to path.
// Nodes not appearing in results are left unchanged.
func (t Tester) UpdateLatencies(nl *NodeList, results []ProbeResult, path string) error {
	now := t.Now()
	byID := make(map[int64]ProbeResult, len(results))
	for _, r := range results {
		byID[r.NodeID] = r
	}
	for i := range nl.Nodes {
		r, ok := byID[nl.Nodes[i].ID]
		if !ok {
			continue
		}
		ts := now
		nl.Nodes[i].LastChecked = &ts
		if r.Err != nil {
			nl.Nodes[i].LastLatency = 0
		} else {
			nl.Nodes[i].LastLatency = r.Latency.Milliseconds()
		}
	}
	return nl.Save(path)
}

// DefaultDialer performs a TCP connect through SOCKS5 (no auth).
// This is the same algorithm as health.DefaultSOCKSProber but as a Dialer.
func DefaultDialer(socksAddr, target string) (time.Duration, error) {
	start := time.Now()

	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return 0, fmt.Errorf("socks5 dial: parse target %q: %w", target, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("socks5 dial: invalid target port %q", portStr)
	}

	conn, err := net.DialTimeout("tcp", socksAddr, 5*time.Second)
	if err != nil {
		return 0, fmt.Errorf("socks5 dial: dial proxy: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

	// SOCKS5 greeting: version=5, nmethods=1, method=0 (no auth)
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return 0, fmt.Errorf("socks5 dial: greeting: %w", err)
	}
	resp := make([]byte, 2)
	if _, err := conn.Read(resp); err != nil {
		return 0, fmt.Errorf("socks5 dial: greeting resp: %w", err)
	}
	if resp[0] != 0x05 || resp[1] != 0x00 {
		return 0, fmt.Errorf("socks5 dial: unexpected greeting response %x", resp)
	}

	// SOCKS5 CONNECT request: version=5, cmd=1 (CONNECT), rsv=0, atyp=3 (DOMAINNAME)
	hostBytes := []byte(host)
	req := make([]byte, 0, 7+len(hostBytes))
	req = append(req, 0x05, 0x01, 0x00, 0x03, byte(len(hostBytes)))
	req = append(req, hostBytes...)
	portBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(portBuf, uint16(port))
	req = append(req, portBuf...)
	if _, err := conn.Write(req); err != nil {
		return 0, fmt.Errorf("socks5 dial: connect request: %w", err)
	}

	// Read connect reply (at least 10 bytes for IPv4 reply)
	reply := make([]byte, 10)
	if _, err := conn.Read(reply); err != nil {
		return 0, fmt.Errorf("socks5 dial: connect reply: %w", err)
	}
	if reply[1] != 0x00 {
		return 0, fmt.Errorf("socks5 dial: connect refused, code %d", reply[1])
	}

	return time.Since(start), nil
}
