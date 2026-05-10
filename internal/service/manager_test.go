package service_test

import (
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/service"
)

func TestAllServicesSingleMode(t *testing.T) {
	m := service.NewManager(config.DefaultPaths())
	svcs := m.AllServices(config.ModeSingle)

	want := []string{"teleproxy.service", "tgproxy-panel.service"}
	if len(svcs) != len(want) {
		t.Fatalf("AllServices(single) returned %d services, want %d", len(svcs), len(want))
	}
	for i, s := range svcs {
		if s != want[i] {
			t.Errorf("service[%d] = %q, want %q", i, s, want[i])
		}
	}
}

func TestAllServicesBridgeMode(t *testing.T) {
	m := service.NewManager(config.DefaultPaths())
	svcs := m.AllServices(config.ModeBridge)

	want := []string{"teleproxy.service", "tgproxy-panel.service", "sing-box.service"}
	if len(svcs) != len(want) {
		t.Fatalf("AllServices(bridge) returned %d services, want %d", len(svcs), len(want))
	}
	for i, s := range svcs {
		if s != want[i] {
			t.Errorf("service[%d] = %q, want %q", i, s, want[i])
		}
	}
}

func TestAllServicesDefaultMode(t *testing.T) {
	m := service.NewManager(config.DefaultPaths())
	svcs := m.AllServices(config.Mode(""))

	want := []string{"teleproxy.service", "tgproxy-panel.service"}
	if len(svcs) != len(want) {
		t.Fatalf("AllServices(unknown) returned %d services, want %d", len(svcs), len(want))
	}
	for i, s := range svcs {
		if s != want[i] {
			t.Errorf("service[%d] = %q, want %q", i, s, want[i])
		}
	}
}
