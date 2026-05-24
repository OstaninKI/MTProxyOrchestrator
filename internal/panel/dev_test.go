package panel

import (
	"testing"
)

func TestBridgeExecDefaultsToRealExecutor(t *testing.T) {
	srv := &Server{}
	got := srv.bridgeExec()
	if _, ok := got.(realBridgeExecutor); !ok {
		t.Fatalf("want realBridgeExecutor, got %T", got)
	}
}

func TestBridgeExecUsesInjectedExecutor(t *testing.T) {
	srv := &Server{BridgeExec: noopBridgeExecutor{}}
	got := srv.bridgeExec()
	if _, ok := got.(noopBridgeExecutor); !ok {
		t.Fatalf("want noopBridgeExecutor, got %T", got)
	}
}

func TestNoopBridgeExecutorAllMethodsSucceed(t *testing.T) {
	e := noopBridgeExecutor{}

	if err := e.WriteFile("/any/path", []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := e.Download("http://example.com", "abc123", "/tmp/dest"); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if err := e.EnableService("sing-box.service"); err != nil {
		t.Fatalf("EnableService: %v", err)
	}
	if err := e.StartService("sing-box.service"); err != nil {
		t.Fatalf("StartService: %v", err)
	}
	if err := e.StopService("sing-box.service"); err != nil {
		t.Fatalf("StopService: %v", err)
	}
	if err := e.DisableService("sing-box.service"); err != nil {
		t.Fatalf("DisableService: %v", err)
	}
	if err := e.ReloadService("sing-box.service"); err != nil {
		t.Fatalf("ReloadService: %v", err)
	}
	active, err := e.ServiceActive("sing-box.service")
	if err != nil {
		t.Fatalf("ServiceActive: %v", err)
	}
	if active {
		t.Fatal("ServiceActive: want false, got true")
	}
}
