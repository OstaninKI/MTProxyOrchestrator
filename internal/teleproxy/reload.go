package teleproxy

import (
	"context"
	"os/exec"
	"time"
)

// CommandRunner runs an external command. It is the injection point for tests.
type CommandRunner func(ctx context.Context, name string, args ...string) error

// DefaultCommandTimeout bounds systemctl invocations so a hung systemd cannot
// block callers (notably HTTP handlers).
const DefaultCommandTimeout = 10 * time.Second

// DefaultCommandRunner runs the command via exec.CommandContext with the given context.
func DefaultCommandRunner(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
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
	Runner      CommandRunner
	Timeout     time.Duration
}

// DefaultController returns a SystemdController for "teleproxy.service".
func DefaultController() *SystemdController {
	return &SystemdController{ServiceName: "teleproxy.service"}
}

func (c *SystemdController) runner() CommandRunner {
	if c.Runner != nil {
		return c.Runner
	}
	return DefaultCommandRunner
}

func (c *SystemdController) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultCommandTimeout
}

func (c *SystemdController) run(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()
	return c.runner()(ctx, name, args...)
}

func (c *SystemdController) Reload() error {
	return c.run("systemctl", "reload-or-restart", c.ServiceName)
}

func (c *SystemdController) Restart() error {
	return c.run("systemctl", "restart", c.ServiceName)
}

func (c *SystemdController) Status() (bool, error) {
	err := c.run("systemctl", "is-active", c.ServiceName)
	return err == nil, nil
}
