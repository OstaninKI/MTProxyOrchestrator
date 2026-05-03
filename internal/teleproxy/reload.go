package teleproxy

import "os/exec"

// ExecCommand is injectable for tests. It defaults to running the command via exec.Command.
var ExecCommand = func(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

// ServiceController manages the teleproxy systemd service.
type ServiceController interface {
	Reload() error  // reload config without dropping connections (SIGHUP if supported, else restart)
	Restart() error // full restart
	Status() (active bool, err error)
}

// SystemdController is the production ServiceController backed by systemctl.
type SystemdController struct {
	ServiceName string // "teleproxy.service"
}

// DefaultController returns a SystemdController for "teleproxy.service".
func DefaultController() *SystemdController {
	return &SystemdController{ServiceName: "teleproxy.service"}
}

func (c *SystemdController) Reload() error {
	// reload-or-restart: sends SIGHUP if ExecReload defined, otherwise restarts.
	// This minimizes connection drops by attempting graceful reload first.
	return ExecCommand("systemctl", "reload-or-restart", c.ServiceName)
}

func (c *SystemdController) Restart() error {
	return ExecCommand("systemctl", "restart", c.ServiceName)
}

func (c *SystemdController) Status() (bool, error) {
	err := ExecCommand("systemctl", "is-active", c.ServiceName)
	return err == nil, nil
}
