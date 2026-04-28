package health

import (
	"fmt"
	"net/http"
	"os/exec"
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

// Checker runs health checks for a given mode.
type Checker struct {
	Systemd SystemdQuerier
	HTTP    HTTPProber
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
	}
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
