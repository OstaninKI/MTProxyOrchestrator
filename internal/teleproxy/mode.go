package teleproxy

import (
	"os"
	"strings"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
)

// DetectMode infers the current runtime mode from the rendered teleproxy config.
// Bridge mode is active when a SOCKS5 upstream is configured.
func DetectMode(path string) (config.Mode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if strings.Contains(string(data), `socks5 = "`) {
		return config.ModeBridge, nil
	}
	return config.ModeSingle, nil
}
