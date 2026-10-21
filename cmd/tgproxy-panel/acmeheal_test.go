package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHealACMESetup(t *testing.T) {
	dir := t.TempDir()
	snippet := filepath.Join(dir, "acme-challenge.conf")
	webRoot := filepath.Join(dir, "tgproxy-acme")

	calls := 0
	orig := startReconcile
	startReconcile = func(context.Context, string) error { calls++; return nil }
	t.Cleanup(func() { startReconcile = orig })

	heal := func() bool {
		return healACMESetup(context.Background(), snippet, webRoot, "/usr/local/bin/tgproxy-cli")
	}

	// Snippet written by a pre-fix reconcile still points into /etc/tgproxy.
	if err := os.WriteFile(snippet, []byte("location /.well-known/acme-challenge/ {\n    root /etc/tgproxy/certs/.well-known-webroot;\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !heal() || calls != 1 {
		t.Fatalf("old snippet: want reconcile triggered, calls=%d", calls)
	}

	if err := os.WriteFile(snippet, []byte("location /.well-known/acme-challenge/ {\n    root "+webRoot+";\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !heal() || calls != 2 {
		t.Fatalf("missing webroot dir: want reconcile triggered, calls=%d", calls)
	}

	if err := os.Mkdir(webRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if heal() || calls != 2 {
		t.Fatalf("healthy setup: want no reconcile, calls=%d", calls)
	}
}
