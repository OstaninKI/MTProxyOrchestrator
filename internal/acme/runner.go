package acme

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"time"
)

// NginxReloader is called after a certificate is renewed so nginx picks up the new files.
type NginxReloader func(ctx context.Context) error

// SystemNginxReloader returns a NginxReloader that runs systemctl reload nginx.
func SystemNginxReloader() NginxReloader {
	return func(ctx context.Context) error {
		return exec.CommandContext(ctx, "systemctl", "reload", "nginx").Run()
	}
}

// Runner orchestrates certificate lifecycle using a Manager.
// It selects the appropriate HTTP-01 challenge mode for each operation and
// reloads nginx after a successful renewal.
type Runner struct {
	Manager    Manager
	WebRootDir string        // dir nginx serves for webroot challenge; only used during renewals
	Reloader   NginxReloader // called after renewal; nil = skip reload
}

// DefaultRunner returns a Runner wired for production use.
func DefaultRunner(mgr Manager, webRootDir string) Runner {
	return Runner{
		Manager:    mgr,
		WebRootDir: webRootDir,
		Reloader:   SystemNginxReloader(),
	}
}

// ObtainCert obtains a new Let's Encrypt certificate using a standalone HTTP server on port 80.
// Suitable for install-time use when nginx is not yet running.
func (r Runner) ObtainCert(ctx context.Context, domain, email string) (certPath, keyPath string, err error) {
	return r.Manager.ObtainACME(ctx, domain, email, ChallengeStandalone, "")
}

// Renew always renews the certificate for domain using the webroot challenge.
// It overwrites the existing cert files and reloads nginx.
// Suitable for forced manual renewal from the panel.
func (r Runner) Renew(ctx context.Context, domain, email string) error {
	certPath := filepath.Join(r.Manager.CertDir, domain, "cert.pem")
	keyPath := filepath.Join(r.Manager.CertDir, domain, "key.pem")
	_, _, err := r.Manager.ObtainACME(ctx, domain, email, ChallengeWebroot, r.WebRootDir)
	if err != nil {
		return fmt.Errorf("renew certificate for %s: %w", domain, err)
	}
	_ = certPath
	_ = keyPath
	return r.reload(ctx)
}

// RenewIfNeeded renews the certificate only when it expires within 30 days.
// Uses the webroot challenge (nginx must be running on port 80).
// Returns true if renewal was performed.
func (r Runner) RenewIfNeeded(ctx context.Context, domain, email, certPath string) (bool, error) {
	info, err := r.Manager.ReadCertInfo(certPath, CertModeACME)
	if err != nil {
		return false, fmt.Errorf("read cert info for %s: %w", domain, err)
	}
	if !r.Manager.NeedsRenewal(info) {
		return false, nil
	}
	if err := r.Renew(ctx, domain, email); err != nil {
		return false, err
	}
	return true, nil
}

// StartRenewalLoop runs a background goroutine that calls RenewIfNeeded on the given interval.
// The goroutine stops when ctx is cancelled. Errors are logged and the loop continues.
func (r Runner) StartRenewalLoop(ctx context.Context, domain, email, certPath string, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				renewed, err := r.RenewIfNeeded(ctx, domain, email, certPath)
				if err != nil {
					slog.Error("certificate renewal failed", "domain", domain, "err", err)
				} else if renewed {
					slog.Info("certificate renewed", "domain", domain)
				}
			}
		}
	}()
}

func (r Runner) reload(ctx context.Context) error {
	if r.Reloader == nil {
		return nil
	}
	return r.Reloader(ctx)
}
