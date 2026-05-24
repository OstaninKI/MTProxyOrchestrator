package main

import (
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
