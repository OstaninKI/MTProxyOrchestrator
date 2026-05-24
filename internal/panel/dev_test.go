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
	srv := &Server{BridgeExec: realBridgeExecutor{}} // placeholder; will use noop in Task 2
	got := srv.bridgeExec()
	if _, ok := got.(realBridgeExecutor); !ok {
		t.Fatalf("want realBridgeExecutor (injected), got %T", got)
	}
}
