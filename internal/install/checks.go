package install

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// CheckError is a single failed preflight check.
type CheckError struct {
	Check       string
	Description string
	Remediation string
}

func (e CheckError) Error() string { return e.Description }

// CheckResult is the aggregate output of Run.
type CheckResult struct {
	Errors []CheckError
}

func (r CheckResult) OK() bool { return len(r.Errors) == 0 }

// Function types for injectable OS dependencies.
type OSReader func() ([]byte, error)
type PortChecker func(port int) error
type DiskStatFn func(path string) (availableBytes uint64, err error)
type MemReader func() ([]byte, error)

// Checker holds injectable dependencies for preflight checks.
type Checker struct {
	ReadOS       OSReader
	CheckPort    PortChecker
	DiskStat     DiskStatFn
	ReadMem      MemReader
	GetUID       func() int
	CheckSystemd func() error
}

// DefaultChecker returns a Checker wired to real OS calls.
func DefaultChecker() Checker {
	return Checker{
		ReadOS: func() ([]byte, error) {
			return os.ReadFile("/etc/os-release")
		},
		CheckPort: func(port int) error {
			ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
			if err != nil {
				return err
			}
			ln.Close()
			return nil
		},
		DiskStat: func(path string) (uint64, error) {
			var stat syscall.Statfs_t
			if err := syscall.Statfs(path, &stat); err != nil {
				return 0, err
			}
			return uint64(stat.Bavail) * uint64(stat.Bsize), nil
		},
		ReadMem: func() ([]byte, error) {
			return os.ReadFile("/proc/meminfo")
		},
		GetUID: os.Getuid,
		CheckSystemd: func() error {
			_, err := os.Stat("/run/systemd/private")
			return err
		},
	}
}

// Run executes all preflight checks and returns the aggregate result.
func (c Checker) Run(panelPort int) CheckResult {
	var result CheckResult

	add := func(check, desc, remediation string) {
		result.Errors = append(result.Errors, CheckError{
			Check:       check,
			Description: desc,
			Remediation: remediation,
		})
	}

	// ubuntu-version
	if data, err := c.ReadOS(); err != nil {
		add("ubuntu-version", "cannot read OS release: "+err.Error(), "Upgrade to Ubuntu 22.04 or later")
	} else if !validUbuntu(data) {
		add("ubuntu-version", "OS is not Ubuntu 22.04 or later", "Upgrade to Ubuntu 22.04 or later")
	}

	// root
	if c.GetUID() != 0 {
		add("root", "not running as root", "Run as root (sudo)")
	}

	// systemd
	if err := c.CheckSystemd(); err != nil {
		add("systemd", "systemd not detected", "systemd is required")
	}

	// port-443
	if err := c.CheckPort(443); err != nil {
		add("port-443", "port 443 is in use", "Free port 443 before installing")
	}

	// panel-port
	if err := c.CheckPort(panelPort); err != nil {
		add("panel-port", fmt.Sprintf("port %d is in use", panelPort), fmt.Sprintf("Free port %d before installing", panelPort))
	}

	// ram
	if data, err := c.ReadMem(); err != nil {
		add("ram", "cannot read memory info: "+err.Error(), "Minimum 256 MB RAM required")
	} else if kb, ok := parseMemTotal(data); !ok || kb < 256*1024 {
		add("ram", "insufficient RAM", "Minimum 256 MB RAM required")
	}

	// disk
	if avail, err := c.DiskStat("/"); err != nil {
		add("disk", "cannot read disk stats: "+err.Error(), "Minimum 1 GB free disk space required")
	} else if avail < 1*1024*1024*1024 {
		add("disk", "insufficient free disk space", "Minimum 1 GB free disk space required")
	}

	return result
}

func validUbuntu(data []byte) bool {
	var isUbuntu bool
	var major, minor int
	var foundVersion bool

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "ID=ubuntu" {
			isUbuntu = true
			continue
		}
		if strings.HasPrefix(line, "VERSION_ID=") {
			val := strings.TrimPrefix(line, "VERSION_ID=")
			val = strings.Trim(val, `"`)
			parts := strings.SplitN(val, ".", 2)
			if len(parts) == 2 {
				maj, err1 := strconv.Atoi(parts[0])
				min, err2 := strconv.Atoi(parts[1])
				if err1 == nil && err2 == nil {
					major, minor = maj, min
					foundVersion = true
				}
			}
		}
	}

	if !isUbuntu || !foundVersion {
		return false
	}
	return major > 22 || (major == 22 && minor >= 4)
}

func parseMemTotal(data []byte) (uint64, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil {
					return kb, true
				}
			}
		}
	}
	return 0, false
}
