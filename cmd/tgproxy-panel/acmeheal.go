package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
)

// acmeSetupStale reports whether nginx and this host are not yet wired for the
// webroot the panel renews into: the challenge snippet must serve webRoot and
// the directory must exist. Installs updated by an older tgproxy-cli keep the
// pre-fix snippet (pointing into the 0700 /etc/tgproxy tree, which nginx
// answers with 403), because that CLI reconciles with its own old templates.
func acmeSetupStale(snippetPath, webRoot string) bool {
	snippet, err := os.ReadFile(snippetPath)
	if err != nil || !bytes.Contains(snippet, []byte("root "+webRoot+";")) {
		return true
	}
	_, err = os.Stat(webRoot)
	return err != nil
}

// startReconcile runs `tgproxy-cli reconcile` as a transient systemd unit. The
// panel itself cannot do it: ProtectSystem=strict keeps /etc/nginx and the new
// webroot read-only here, and reconcile restarts this very service, so it must
// live outside the panel's cgroup. --collect lets a failed run be retried on
// the next renewal tick.
var startReconcile = func(ctx context.Context, cliBin string) error {
	return exec.CommandContext(ctx, "systemd-run", "--unit=tgproxy-acme-reconcile", "--collect", cliBin, "reconcile").Run()
}

// healACMESetup triggers a reconcile when the ACME setup is stale and reports
// whether it did, so the caller skips a renewal that would fail anyway.
func healACMESetup(ctx context.Context, snippetPath, webRoot, cliBin string) bool {
	if !acmeSetupStale(snippetPath, webRoot) {
		return false
	}
	if err := startReconcile(ctx, cliBin); err != nil {
		slog.Error("acme setup is stale; reconcile failed to start", "err", err)
	} else {
		slog.Info("acme setup is stale; started tgproxy-cli reconcile")
	}
	return true
}
