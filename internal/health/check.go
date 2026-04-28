package health

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"time"
)

// ServiceState describes one monitored service.
type ServiceState struct {
	Name    string
	Active  bool
	Message string
}

// Status is the aggregate health result for a mode.
type Status struct {
	Services []ServiceState
	OK       bool
	Summary  string
}

// SystemdQuerier lets tests inject a fake systemd state reader.
// Returns (true, nil) when the service is active and healthy.
type SystemdQuerier func(serviceName string) (active bool, err error)

// HTTPProber lets tests inject a fake HTTP probe.
// Returns nil when the URL is reachable and returns a successful response.
type HTTPProber func(url string) error

// SOCKSProber lets tests inject a fake SOCKS5 connectivity probe.
// It should attempt a TCP connect through the SOCKS5 proxy at socksAddr to target.
// Returns the observed round-trip duration and any error.
type SOCKSProber func(socksAddr, target string) (latency time.Duration, err error)

// Checker runs health checks for a given mode.
type Checker struct {
	Systemd SystemdQuerier
	HTTP    HTTPProber
	SOCKS5  SOCKSProber // used by CheckBridge; defaults to DefaultSOCKSProber if nil
}

// DefaultChecker returns a Checker wired to real system calls.
func DefaultChecker() Checker {
	return Checker{
		Systemd: func(name string) (bool, error) {
			err := exec.Command("systemctl", "is-active", name).Run()
			return err == nil, nil
		},
		HTTP: func(url string) error {
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(url)
			if err != nil {
				return err
			}
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				return fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			return nil
		},
		SOCKS5: DefaultSOCKSProber,
	}
}

// DefaultSOCKSProber dials through a SOCKS5 proxy (no auth) to target and
// measures the round-trip time of the TCP connect through the proxy.
// socksAddr is "host:port" of the SOCKS5 server; target is "host:port" to reach.
func DefaultSOCKSProber(socksAddr, target string) (time.Duration, error) {
	start := time.Now()

	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return 0, fmt.Errorf("socks5 probe: parse target %q: %w", target, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("socks5 probe: invalid target port %q", portStr)
	}

	conn, err := net.DialTimeout("tcp", socksAddr, 5*time.Second)
	if err != nil {
		return 0, fmt.Errorf("socks5 probe: dial proxy: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

	// SOCKS5 greeting: version=5, nmethods=1, method=0 (no auth)
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return 0, fmt.Errorf("socks5 probe: greeting: %w", err)
	}
	resp := make([]byte, 2)
	if _, err := conn.Read(resp); err != nil {
		return 0, fmt.Errorf("socks5 probe: greeting resp: %w", err)
	}
	if resp[0] != 0x05 || resp[1] != 0x00 {
		return 0, fmt.Errorf("socks5 probe: unexpected greeting response %x", resp)
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
		return 0, fmt.Errorf("socks5 probe: connect request: %w", err)
	}

	// Read connect reply (at least 10 bytes for IPv4 reply)
	reply := make([]byte, 10)
	if _, err := conn.Read(reply); err != nil {
		return 0, fmt.Errorf("socks5 probe: connect reply: %w", err)
	}
	if reply[1] != 0x00 {
		return 0, fmt.Errorf("socks5 probe: connect refused, code %d", reply[1])
	}

	return time.Since(start), nil
}

// CheckSingle checks Single mode health:
//  1. teleproxy.service is active (via Systemd)
//  2. nginx loopback is reachable: HTTP GET http://127.0.0.1:80/ (via HTTP)
//
// Status.OK is true only when all checks pass.
func (c Checker) CheckSingle() Status {
	var services []ServiceState
	ok := true

	active, err := c.Systemd("teleproxy.service")
	tpState := ServiceState{Name: "teleproxy.service", Active: active && err == nil}
	if err != nil {
		tpState.Message = "systemd query failed: " + err.Error()
	} else if !active {
		tpState.Message = "service is not active"
	} else {
		tpState.Message = "running"
	}
	if !tpState.Active {
		ok = false
	}
	services = append(services, tpState)

	ngErr := c.HTTP("http://127.0.0.1:80/")
	ngState := ServiceState{Name: "nginx-stub", Active: ngErr == nil}
	if ngErr != nil {
		ngState.Message = "loopback probe failed: " + ngErr.Error()
	} else {
		ngState.Message = "reachable"
	}
	if !ngState.Active {
		ok = false
	}
	services = append(services, ngState)

	summary := "healthy"
	if !ok {
		summary = "unhealthy"
	}
	return Status{Services: services, OK: ok, Summary: summary}
}

// BridgeStepStatus holds the result of a single chain check step.
type BridgeStepStatus struct {
	Name    string
	OK      bool
	Latency time.Duration // 0 when not applicable
	Message string
}

// BridgeStatus is the aggregate health result for Bridge mode.
type BridgeStatus struct {
	Steps   []BridgeStepStatus
	OK      bool
	Summary string
}

const (
	singboxSOCKSAddr    = "127.0.0.1:1080"
	telegramChainTarget = "149.154.167.51:443" // Telegram DC1; TCP-only probe, no credentials
)

// CheckBridge checks Bridge mode chain health:
//  1. teleproxy.service is active.
//  2. sing-box.service is active.
//  3. Local SOCKS5 inbound (sing-box) is reachable.
//  4. A Telegram DC is reachable through the SOCKS5 chain.
//
// Step 4 uses a bare TCP connect through SOCKS5 — no Telegram authentication
// or private data is involved.
func (c Checker) CheckBridge() BridgeStatus {
	var steps []BridgeStepStatus
	ok := true

	prober := c.SOCKS5
	if prober == nil {
		prober = DefaultSOCKSProber
	}

	// Step 1: teleproxy.service active
	steps = append(steps, c.checkService("teleproxy.service"))
	if !steps[len(steps)-1].OK {
		ok = false
	}

	// Step 2: sing-box.service active
	steps = append(steps, c.checkService("sing-box.service"))
	if !steps[len(steps)-1].OK {
		ok = false
	}

	// Step 3: SOCKS5 loopback reachable (plain TCP to proxy port — no relay target)
	latency, err := prober(singboxSOCKSAddr, "127.0.0.1:1080")
	socksStep := BridgeStepStatus{Name: "socks5-inbound"}
	if err != nil {
		// Plain TCP connect to loopback is the minimal check; treat failure as down.
		// Try a simpler TCP dial to the SOCKS5 port directly.
		start := time.Now()
		conn, dialErr := net.DialTimeout("tcp", singboxSOCKSAddr, 3*time.Second)
		if dialErr == nil {
			conn.Close()
			socksStep.OK = true
			socksStep.Latency = time.Since(start)
			socksStep.Message = "reachable"
		} else {
			socksStep.OK = false
			socksStep.Message = "SOCKS5 inbound not reachable: " + dialErr.Error()
			ok = false
		}
	} else {
		socksStep.OK = true
		socksStep.Latency = latency
		socksStep.Message = "reachable"
	}
	steps = append(steps, socksStep)

	// Step 4: Telegram chain reachability through SOCKS5
	chainLatency, chainErr := prober(singboxSOCKSAddr, telegramChainTarget)
	chainStep := BridgeStepStatus{Name: "telegram-chain", Latency: chainLatency}
	if chainErr != nil {
		chainStep.OK = false
		chainStep.Message = "chain unreachable: " + chainErr.Error()
		ok = false
	} else {
		chainStep.OK = true
		chainStep.Message = fmt.Sprintf("reachable in %dms", chainLatency.Milliseconds())
	}
	steps = append(steps, chainStep)

	summary := "bridge healthy"
	if !ok {
		summary = "bridge unhealthy"
	}
	return BridgeStatus{Steps: steps, OK: ok, Summary: summary}
}

// checkService queries one systemd service and returns a BridgeStepStatus.
func (c Checker) checkService(name string) BridgeStepStatus {
	active, err := c.Systemd(name)
	s := BridgeStepStatus{Name: name, OK: active && err == nil}
	switch {
	case err != nil:
		s.Message = "systemd query failed: " + err.Error()
	case !active:
		s.Message = "not active"
	default:
		s.Message = "running"
	}
	return s
}
