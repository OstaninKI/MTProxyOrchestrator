package panel

import (
	"os"
)

// noopBridgeExecutor implements bridge.Executor with no-op operations.
// Used in dev mode so bridge enable/disable handlers succeed without touching the OS.
type noopBridgeExecutor struct{}

func (noopBridgeExecutor) WriteFile(_ string, _ []byte, _ os.FileMode) error { return nil }
func (noopBridgeExecutor) Download(_, _, _ string) error                     { return nil }
func (noopBridgeExecutor) EnableService(_ string) error                      { return nil }
func (noopBridgeExecutor) StartService(_ string) error                       { return nil }
func (noopBridgeExecutor) StopService(_ string) error                        { return nil }
func (noopBridgeExecutor) DisableService(_ string) error                     { return nil }
func (noopBridgeExecutor) ReloadService(_ string) error                      { return nil }
func (noopBridgeExecutor) ServiceActive(_ string) (bool, error)              { return false, nil }
