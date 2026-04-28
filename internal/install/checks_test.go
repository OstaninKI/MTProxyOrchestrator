package install_test

import (
	"errors"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
)

const goodOS2204 = `NAME="Ubuntu"
ID=ubuntu
VERSION_ID="22.04"
PRETTY_NAME="Ubuntu 22.04 LTS"
`

const goodOS2404 = `NAME="Ubuntu"
ID=ubuntu
VERSION_ID="24.04"
PRETTY_NAME="Ubuntu 24.04 LTS"
`

const badOS2004 = `NAME="Ubuntu"
ID=ubuntu
VERSION_ID="20.04"
PRETTY_NAME="Ubuntu 20.04 LTS"
`

const badOSDebian = `NAME="Debian GNU/Linux"
ID=debian
VERSION_ID="12"
PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
`

const goodMem = `MemTotal:        524288 kB
MemFree:         262144 kB
`

const lowMem = `MemTotal:        131072 kB
MemFree:          65536 kB
`

func baseChecker(osData string) install.Checker {
	return install.Checker{
		ReadOS:       func() ([]byte, error) { return []byte(osData), nil },
		CheckPort:    func(port int) error { return nil },
		DiskStat:     func(path string) (uint64, error) { return 2 * 1024 * 1024 * 1024, nil },
		ReadMem:      func() ([]byte, error) { return []byte(goodMem), nil },
		GetUID:       func() int { return 0 },
		CheckSystemd: func() error { return nil },
	}
}

func hasCheck(result install.CheckResult, check string) bool {
	for _, e := range result.Errors {
		if e.Check == check {
			return true
		}
	}
	return false
}

func TestUbuntu2204Good(t *testing.T) {
	c := baseChecker(goodOS2204)
	result := c.Run(8443)
	if !result.OK() {
		t.Fatalf("expected OK, got errors: %v", result.Errors)
	}
}

func TestUbuntu2404Good(t *testing.T) {
	c := baseChecker(goodOS2404)
	result := c.Run(8443)
	if !result.OK() {
		t.Fatalf("expected OK, got errors: %v", result.Errors)
	}
}

func TestUbuntu2004Bad(t *testing.T) {
	c := baseChecker(badOS2004)
	result := c.Run(8443)
	if !hasCheck(result, "ubuntu-version") {
		t.Fatal("expected ubuntu-version error for Ubuntu 20.04")
	}
}

func TestNonUbuntu(t *testing.T) {
	c := baseChecker(badOSDebian)
	result := c.Run(8443)
	if !hasCheck(result, "ubuntu-version") {
		t.Fatal("expected ubuntu-version error for Debian")
	}
}

func TestPort443Busy(t *testing.T) {
	c := baseChecker(goodOS2204)
	c.CheckPort = func(port int) error {
		if port == 443 {
			return errors.New("address already in use")
		}
		return nil
	}
	result := c.Run(8443)
	if !hasCheck(result, "port-443") {
		t.Fatal("expected port-443 error")
	}
}

func TestPanelPortBusy(t *testing.T) {
	c := baseChecker(goodOS2204)
	c.CheckPort = func(port int) error {
		if port == 8443 {
			return errors.New("address already in use")
		}
		return nil
	}
	result := c.Run(8443)
	if !hasCheck(result, "panel-port") {
		t.Fatal("expected panel-port error")
	}
}

func TestLowRAM(t *testing.T) {
	c := baseChecker(goodOS2204)
	c.ReadMem = func() ([]byte, error) { return []byte(lowMem), nil }
	result := c.Run(8443)
	if !hasCheck(result, "ram") {
		t.Fatal("expected ram error for 128 MB")
	}
}

func TestLowDisk(t *testing.T) {
	c := baseChecker(goodOS2204)
	c.DiskStat = func(path string) (uint64, error) { return 512 * 1024 * 1024, nil }
	result := c.Run(8443)
	if !hasCheck(result, "disk") {
		t.Fatal("expected disk error for 512 MB")
	}
}

func TestAllPass(t *testing.T) {
	c := baseChecker(goodOS2204)
	result := c.Run(8443)
	if !result.OK() {
		t.Fatalf("expected all checks to pass, got: %v", result.Errors)
	}
}
