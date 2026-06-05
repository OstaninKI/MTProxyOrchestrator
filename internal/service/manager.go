package service

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
)

const systemctlTimeout = 10 * time.Second

const (
	defaultHealthCheckTimeout      = 15 * time.Second
	defaultHealthCheckPollInterval = 250 * time.Millisecond
)

type HealthResult struct {
	Service string
	OK      bool
	Error   error
}

type Manager struct {
	Paths config.InstallPaths
}

func NewManager(paths config.InstallPaths) *Manager {
	return &Manager{Paths: paths}
}

func (m *Manager) AllServices(mode config.Mode) []string {
	switch mode {
	case config.ModeBridge:
		return []string{"teleproxy.service", "tgproxy-panel.service", "sing-box.service"}
	default:
		return []string{"teleproxy.service", "tgproxy-panel.service"}
	}
}

func (m *Manager) Restart(service string) error {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "restart", service)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart %s: %s: %w", service, out, err)
	}
	return nil
}

func (m *Manager) HealthCheck(service string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastState string
	for {
		state, err := systemctlIsActive(service)
		lastState = state
		if err == nil && state == "active" {
			return nil
		}
		if state == "failed" {
			return fmt.Errorf("service %s is failed", service)
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(defaultHealthCheckPollInterval)
	}
	if lastState == "" {
		return fmt.Errorf("service %s is not active", service)
	}
	return fmt.Errorf("service %s is not active (state=%s)", service, lastState)
}

func (m *Manager) RestartAll(mode config.Mode) []HealthResult {
	services := m.AllServices(mode)
	results := make([]HealthResult, 0, len(services))
	for _, svc := range services {
		r := HealthResult{Service: svc}
		if err := m.Restart(svc); err != nil {
			r.Error = err
			r.OK = false
			results = append(results, r)
			continue
		}
		if err := m.HealthCheck(svc, defaultHealthCheckTimeout); err != nil {
			r.Error = err
			r.OK = false
		} else {
			r.OK = true
		}
		results = append(results, r)
	}
	return results
}

func (m *Manager) DaemonReload() error {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "daemon-reload")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %s: %w", out, err)
	}
	return nil
}

// RestartNginx validates the nginx configuration and then restarts the service.
//
// A plain reload (SIGHUP) is not enough when a config change alters proxy
// behavior (e.g. adding WebSocket upgrade handling): nginx keeps old worker
// processes alive to drain in-flight client connections, and browsers reuse
// their keepalive / HTTP-2 connection, so they keep hitting an old worker that
// still runs the previous config until they happen to reconnect. A restart
// forces every connection onto a worker with the new config.
//
// The config is validated with `nginx -t` first: if it is invalid we return an
// error and leave the running nginx untouched, rather than restarting into a
// broken config and taking nginx down.
func (m *Manager) RestartNginx() error {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "nginx", "-t").CombinedOutput(); err != nil {
		return fmt.Errorf("nginx -t: %s: %w", out, err)
	}
	return m.Restart("nginx")
}

func systemctlIsActive(service string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), systemctlTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", service).Output()
	state := strings.TrimSpace(string(out))
	if state == "" && err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			state = strings.TrimSpace(string(ee.Stderr))
		}
	}
	return state, err
}
