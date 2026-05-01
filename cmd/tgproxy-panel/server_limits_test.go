package main

import (
	"testing"
	"time"
)

// TestNewPanelHTTPServerTimeouts verifies that the HTTP server built by
// newPanelHTTPServer has all required timeout and size-limit fields set.
func TestNewPanelHTTPServerTimeouts(t *testing.T) {
	srv := newPanelHTTPServer("127.0.0.1:0", nil)

	if srv.ReadHeaderTimeout == 0 {
		t.Error("ReadHeaderTimeout must be non-zero")
	}
	if srv.ReadTimeout == 0 {
		t.Error("ReadTimeout must be non-zero")
	}
	if srv.WriteTimeout == 0 {
		t.Error("WriteTimeout must be non-zero")
	}
	if srv.IdleTimeout == 0 {
		t.Error("IdleTimeout must be non-zero")
	}
	if srv.MaxHeaderBytes == 0 {
		t.Error("MaxHeaderBytes must be non-zero")
	}
}

// TestNewPanelHTTPServerExpectedValues verifies the specific values required
// by the R2 specification.
func TestNewPanelHTTPServerExpectedValues(t *testing.T) {
	srv := newPanelHTTPServer("127.0.0.1:0", nil)

	if got, want := srv.ReadHeaderTimeout, 10*time.Second; got != want {
		t.Errorf("ReadHeaderTimeout = %v, want %v", got, want)
	}
	if got, want := srv.ReadTimeout, 30*time.Second; got != want {
		t.Errorf("ReadTimeout = %v, want %v", got, want)
	}
	if got, want := srv.WriteTimeout, 60*time.Second; got != want {
		t.Errorf("WriteTimeout = %v, want %v", got, want)
	}
	if got, want := srv.IdleTimeout, 120*time.Second; got != want {
		t.Errorf("IdleTimeout = %v, want %v", got, want)
	}
	if got, want := srv.MaxHeaderBytes, 1<<20; got != want {
		t.Errorf("MaxHeaderBytes = %d, want %d", got, want)
	}
	if got, want := srv.Addr, "127.0.0.1:0"; got != want {
		t.Errorf("Addr = %q, want %q", got, want)
	}
}
