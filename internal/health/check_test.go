package health_test

import (
	"errors"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
)

func TestCheckSingleAllOK(t *testing.T) {
	c := health.Checker{
		Systemd: func(name string) (bool, error) { return true, nil },
		HTTP:    func(url string) error { return nil },
	}
	s := c.CheckSingle()
	if !s.OK {
		t.Errorf("expected OK, got: %s, services: %v", s.Summary, s.Services)
	}
}

func TestCheckSingleTeleproxyDown(t *testing.T) {
	c := health.Checker{
		Systemd: func(name string) (bool, error) { return false, nil },
		HTTP:    func(url string) error { return nil },
	}
	s := c.CheckSingle()
	if s.OK {
		t.Error("expected not OK when teleproxy is down")
	}
}

func TestCheckSingleNginxDown(t *testing.T) {
	c := health.Checker{
		Systemd: func(name string) (bool, error) { return true, nil },
		HTTP:    func(url string) error { return errors.New("connection refused") },
	}
	s := c.CheckSingle()
	if s.OK {
		t.Error("expected not OK when nginx is unreachable")
	}
}
