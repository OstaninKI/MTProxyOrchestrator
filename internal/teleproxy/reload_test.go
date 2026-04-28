package teleproxy_test

import (
	"errors"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
)

type fakeController struct {
	reloadErr  error
	restartErr error
	active     bool
}

func (f *fakeController) Reload() error         { return f.reloadErr }
func (f *fakeController) Restart() error        { return f.restartErr }
func (f *fakeController) Status() (bool, error) { return f.active, nil }

// Verify fakeController implements the interface at compile time.
var _ teleproxy.ServiceController = (*fakeController)(nil)

func TestReloadSuccess(t *testing.T) {
	fc := &fakeController{}
	if err := fc.Reload(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestReloadError(t *testing.T) {
	fc := &fakeController{reloadErr: errors.New("systemd unavailable")}
	if err := fc.Reload(); err == nil {
		t.Error("expected error from failed reload")
	}
}

func TestStatusActive(t *testing.T) {
	fc := &fakeController{active: true}
	ok, err := fc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected active=true")
	}
}

func TestStatusInactive(t *testing.T) {
	fc := &fakeController{active: false}
	ok, err := fc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected active=false")
	}
}

func TestDefaultControllerServiceName(t *testing.T) {
	c := teleproxy.DefaultController()
	if c.ServiceName != "teleproxy.service" {
		t.Errorf("ServiceName = %q, want teleproxy.service", c.ServiceName)
	}
}
