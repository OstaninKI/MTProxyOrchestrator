package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/acme"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:     "tgproxy-panel",
	Short:   "MTProto proxy admin panel",
	Version: version.Version,
}

var (
	dbPath      string
	panelPath   string
	listenAddr  string
	mtprotoPort int
	maskHost    string
	statsPort   int
	certDir     string
	domain      string
	acmeEmail   string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the admin panel HTTP server",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().StringVar(&dbPath, "db", "/etc/tgproxy/panel.db", "path to SQLite database")
	serveCmd.Flags().StringVar(&panelPath, "path", "/p-changeme/", "panel URL path prefix")
	serveCmd.Flags().StringVar(&listenAddr, "listen", "127.0.0.1:18080", "listen address")
	serveCmd.Flags().IntVar(&mtprotoPort, "mtproto-port", 443, "MTProto listen port used when rendering Teleproxy config")
	serveCmd.Flags().StringVar(&maskHost, "mask-host", "www.microsoft.com", "FakeTLS mask host used when rendering Teleproxy config")
	serveCmd.Flags().IntVar(&statsPort, "stats-port", 9091, "Teleproxy stats port used when rendering Teleproxy config")
	serveCmd.Flags().StringVar(&certDir, "cert-dir", "/etc/tgproxy/certs", "directory for TLS certificates")
	serveCmd.Flags().StringVar(&domain, "domain", "", "panel domain; enables ACME renewal loop when set with --acme-email")
	serveCmd.Flags().StringVar(&acmeEmail, "acme-email", "", "email for Let's Encrypt renewal notifications")
	rootCmd.AddCommand(serveCmd)
}

// newPanelHTTPServer builds the http.Server used by runServe.
// All timeout and limit fields are set explicitly; callers must not rely on
// zero-value defaults.
func newPanelHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}
}

// applyDBSettings overwrites CLI flag values with any values stored in the DB,
// and populates the retainer's configurable retention fields.
// Errors reading individual settings are silently ignored — flags keep their defaults.
func applyDBSettings(d *db.DB, ret *metrics.Retainer, mtprotoPort *int, maskHost *string) {
	if v := d.GetSetting("mask_host", ""); v != "" {
		*maskHost = v
	}
	if v := d.GetSetting("mtproto_port", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 65535 {
			*mtprotoPort = n
		}
	}
	if v := d.GetSetting("retention_minutes_days", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			ret.MinuteRetentionDays = n
		}
	}
	if v := d.GetSetting("retention_hourly_days", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			ret.HourlyRetentionDays = n
		}
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	d, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer d.Close()

	statsAddr := fmt.Sprintf("http://127.0.0.1:%d", statsPort)
	scraper := metrics.DefaultScraper(statsAddr)
	sampler := metrics.Sampler{
		Source: scraper.Scrape,
		Store:  metrics.DBStoreFn(d),
		Now:    func() int64 { return time.Now().Unix() },
	}
	retainer := metrics.Retainer{DB: d}
	applyDBSettings(d, &retainer, &mtprotoPort, &maskHost)

	// Context that is cancelled on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start metrics sampler goroutine.
	go func() {
		if err := sampler.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "metrics sampler: %v\n", err)
		}
	}()

	// Start retention/aggregation goroutine on a daily schedule.
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := retainer.Run(); err != nil {
					fmt.Fprintf(os.Stderr, "metrics retention: %v\n", err)
				}
			}
		}
	}()

	// Start ACME renewal loop when domain and email are configured.
	if domain != "" && acmeEmail != "" {
		mgr := acme.DefaultManager(d, certDir, "")
		webRootDir := filepath.Join(certDir, ".well-known-webroot")
		runner := acme.DefaultRunner(mgr, webRootDir)
		certPath := filepath.Join(certDir, domain, "cert.pem")
		runner.StartRenewalLoop(ctx, domain, acmeEmail, certPath, 12*time.Hour)
	}

	srv := &panel.Server{
		DB:          d,
		PanelPath:   panelPath,
		RateLimiter: panel.NewRateLimiter(),
		Secure:      true,
		BridgeCfg: &panel.BridgeConfig{
			MTProtoPort: mtprotoPort,
			MaskHost:    maskHost,
			StatsPort:   statsPort,
		},
		SettingsCfg: &panel.SettingsConfig{
			CertDir:   certDir,
			Domain:    domain,
			ACMEEmail: acmeEmail,
		},
	}

	httpSrv := newPanelHTTPServer(listenAddr, srv.Handler())

	// Shut down the HTTP server gracefully when the context is cancelled.
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		//nolint:errcheck
		httpSrv.Shutdown(shutCtx)
	}()

	fmt.Fprintf(cmd.OutOrStdout(), "panel listening on %s%s\n", listenAddr, panelPath)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
