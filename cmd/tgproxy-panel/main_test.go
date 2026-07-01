package main

import (
	"os"
	"testing"
)

func TestDevFlagIsRegistered(t *testing.T) {
	f := serveCmd.Flags().Lookup("dev")
	if f == nil {
		t.Fatal("--dev flag not registered on serve command")
	}
	if f.DefValue != "false" {
		t.Fatalf("--dev default = %q, want \"false\"", f.DefValue)
	}
}

// TestGuardDevModeSafety_LoopbackAllowed verifies the guard accepts the default
// dev listen address. The root check is skipped when the test runner is root.
func TestGuardDevModeSafety_LoopbackAllowed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; dev guard's root branch cannot be exercised")
	}
	saved := listenAddr
	listenAddr = "127.0.0.1:8080"
	defer func() { listenAddr = saved }()
	if err := guardDevModeSafety(); err != nil {
		t.Fatalf("loopback dev mode should be allowed, got: %v", err)
	}
}

// TestGuardDevModeSafety_PublicListenRefused verifies the guard rejects a
// non-loopback bind.
func TestGuardDevModeSafety_PublicListenRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; dev guard's root branch cannot be exercised")
	}
	saved := listenAddr
	listenAddr = "0.0.0.0:8080"
	defer func() { listenAddr = saved }()
	if err := guardDevModeSafety(); err == nil {
		t.Fatal("expected error for non-loopback dev listen, got nil")
	}
}
