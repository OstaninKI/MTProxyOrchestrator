package teleproxy

import "os/exec"

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
	// teleproxy does not support SIGHUP reload; use restart
	return exec.Command("systemctl", "restart", c.ServiceName).Run()
}

func (c *SystemdController) Restart() error {
	return exec.Command("systemctl", "restart", c.ServiceName).Run()
}

func (c *SystemdController) Status() (bool, error) {
	err := exec.Command("systemctl", "is-active", c.ServiceName).Run()
	return err == nil, nil
}
