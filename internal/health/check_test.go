package health_test

import (
	"errors"
	"testing"
	"time"

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

func TestCheckSinglePanelDown(t *testing.T) {
	c := health.Checker{
		Systemd: func(name string) (bool, error) {
			if name == "tgproxy-panel.service" {
				return false, nil
			}
			return true, nil
		},
		HTTP: func(url string) error { return nil },
	}
	s := c.CheckSingle()
	if s.OK {
		t.Error("expected not OK when tgproxy-panel is down")
	}
}

func TestCheckSingleReturnsThreeServices(t *testing.T) {
	c := health.Checker{
		Systemd: func(name string) (bool, error) { return true, nil },
		HTTP:    func(url string) error { return nil },
	}
	s := c.CheckSingle()
	if len(s.Services) != 3 {
		t.Errorf("expected 3 services, got %d: %v", len(s.Services), s.Services)
	}
}

// --- Bridge health checks ---

func okSOCKS(_, _ string) (time.Duration, error)   { return 5 * time.Millisecond, nil }
func failSOCKS(_, _ string) (time.Duration, error) { return 0, errors.New("refused") }

func TestCheckBridgeAllOK(t *testing.T) {
	c := health.Checker{
		Systemd: func(_ string) (bool, error) { return true, nil },
		HTTP:    func(_ string) error { return nil },
		SOCKS5:  okSOCKS,
	}
	s := c.CheckBridge()
	if !s.OK {
		t.Errorf("expected bridge OK, got: %s, steps: %+v", s.Summary, s.Steps)
	}
	if len(s.Steps) == 0 {
		t.Error("expected non-empty steps")
	}
}

func TestCheckBridgeTeleproxyDown(t *testing.T) {
	c := health.Checker{
		Systemd: func(name string) (bool, error) {
			if name == "teleproxy.service" {
				return false, nil
			}
			return true, nil
		},
		SOCKS5: okSOCKS,
	}
	s := c.CheckBridge()
	if s.OK {
		t.Error("expected bridge NOT OK when teleproxy is down")
	}
}

func TestCheckBridgeSingboxDown(t *testing.T) {
	c := health.Checker{
		Systemd: func(name string) (bool, error) {
			if name == "sing-box.service" {
				return false, nil
			}
			return true, nil
		},
		SOCKS5: okSOCKS,
	}
	s := c.CheckBridge()
	if s.OK {
		t.Error("expected bridge NOT OK when sing-box is down")
	}
}

func TestCheckBridgeChainUnreachable(t *testing.T) {
	calls := 0
	c := health.Checker{
		Systemd: func(_ string) (bool, error) { return true, nil },
		SOCKS5: func(_, _ string) (time.Duration, error) {
			calls++
			if calls == 2 { // second call = Telegram chain probe
				return 0, errors.New("chain refused")
			}
			return 5 * time.Millisecond, nil
		},
	}
	s := c.CheckBridge()
	if s.OK {
		t.Error("expected bridge NOT OK when chain is unreachable")
	}
	var chainStep *health.BridgeStepStatus
	for i := range s.Steps {
		if s.Steps[i].Name == "telegram-chain" {
			chainStep = &s.Steps[i]
		}
	}
	if chainStep == nil {
		t.Fatal("telegram-chain step not found")
	}
	if chainStep.OK {
		t.Error("telegram-chain step should not be OK")
	}
}

func TestCheckBridgeReportsLatency(t *testing.T) {
	c := health.Checker{
		Systemd: func(_ string) (bool, error) { return true, nil },
		SOCKS5:  func(_, _ string) (time.Duration, error) { return 42 * time.Millisecond, nil },
	}
	s := c.CheckBridge()
	for _, step := range s.Steps {
		if step.Name == "telegram-chain" && step.Latency == 0 && step.OK {
			t.Error("expected non-zero latency for telegram-chain step")
		}
	}
}
