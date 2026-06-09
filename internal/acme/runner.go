package acme

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sync"
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
//
// Runner tracks per-domain "pending reload" state so a transient nginx reload
// failure after a successful ACME issuance does not cause the renewal loop to
// re-issue the certificate (which would burn Let's Encrypt rate limits). The
// next tick observes the on-disk certificate is still valid and only retries
// the reload step.
type Runner struct {
	Manager    Manager
	WebRootDir string        // dir nginx serves for webroot challenge; only used during renewals
	Reloader   NginxReloader // called after renewal; nil = skip reload

	// RenewEnabled, when non-nil, gates RenewIfNeeded: if it returns false the
	// background loop performs no work. The manual Renew path is never gated.
	RenewEnabled func() bool

	mu             sync.Mutex
	pendingReloads map[string]bool
}

// DefaultRunner returns a Runner wired for production use.
func DefaultRunner(mgr Manager, webRootDir string) *Runner {
	return &Runner{
		Manager:    mgr,
		WebRootDir: webRootDir,
		Reloader:   SystemNginxReloader(),
	}
}

// ObtainCert obtains a new Let's Encrypt certificate using a standalone HTTP server on port 80.
// Suitable for install-time use when nginx is not yet running.
func (r *Runner) ObtainCert(ctx context.Context, domain, email string) (certPath, keyPath string, err error) {
	return r.Manager.ObtainACME(ctx, domain, email, ChallengeStandalone, "")
}

// Renew always renews the certificate for domain using the webroot challenge.
// It overwrites the existing cert files and reloads nginx.
// Suitable for forced manual renewal from the panel.
//
// If the ACME order succeeds but the nginx reload fails, the on-disk cert is
// still considered renewed (the new cert is written before the reload attempt)
// and the domain is marked as having a pending reload. Renew still returns the
// reload error so callers can surface it; the renewal loop checks pending
// state and only retries the reload on the next tick.
func (r *Runner) Renew(ctx context.Context, domain, email string) error {
	certPath := filepath.Join(r.Manager.CertDir, domain, "cert.pem")
	keyPath := filepath.Join(r.Manager.CertDir, domain, "key.pem")
	_, _, err := r.Manager.ObtainACME(ctx, domain, email, ChallengeWebroot, r.WebRootDir)
	if err != nil {
		return fmt.Errorf("renew certificate for %s: %w", domain, err)
	}
	_ = certPath
	_ = keyPath
	if err := r.reload(ctx); err != nil {
		r.setPendingReload(domain, true)
		return fmt.Errorf("reload nginx for %s: %w", domain, err)
	}
	r.setPendingReload(domain, false)
	return nil
}

// RenewIfNeeded renews the certificate only when it expires within 30 days, or
// retries a pending nginx reload if the certificate on disk is still valid but
// a previous renewal left the reload in a failed state.
//
// Returns true if a renewal was performed (ACME order completed) or if a
// pending reload was successfully retried.
func (r *Runner) RenewIfNeeded(ctx context.Context, domain, email, certPath string) (bool, error) {
	if r.RenewEnabled != nil && !r.RenewEnabled() {
		return false, nil
	}
	info, err := r.Manager.ReadCertInfo(certPath, CertModeACME)
	if err != nil {
		return false, fmt.Errorf("read cert info for %s: %w", domain, err)
	}
	if !r.Manager.NeedsRenewal(info) {
		// Cert on disk is still fresh. Retry only the reload if a previous
		// attempt left it pending — never re-issue the cert.
		if r.isPendingReload(domain) {
			if err := r.reload(ctx); err != nil {
				return false, fmt.Errorf("retry reload nginx for %s: %w", domain, err)
			}
			r.setPendingReload(domain, false)
			return true, nil
		}
		return false, nil
	}
	if err := r.Renew(ctx, domain, email); err != nil {
		return false, err
	}
	return true, nil
}

// StartRenewalLoop runs a background goroutine that calls RenewIfNeeded on the given interval.
// The goroutine stops when ctx is cancelled. Errors are logged and the loop continues.
func (r *Runner) StartRenewalLoop(ctx context.Context, domain, email, certPath string, interval time.Duration) {
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

func (r *Runner) reload(ctx context.Context) error {
	if r.Reloader == nil {
		return nil
	}
	return r.Reloader(ctx)
}

func (r *Runner) setPendingReload(domain string, pending bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pendingReloads == nil {
		r.pendingReloads = make(map[string]bool)
	}
	if pending {
		r.pendingReloads[domain] = true
	} else {
		delete(r.pendingReloads, domain)
	}
}

func (r *Runner) isPendingReload(domain string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pendingReloads[domain]
}
