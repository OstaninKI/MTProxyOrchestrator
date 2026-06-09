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
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/quota"
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
	dbPath        string
	panelPath     string
	listenAddr    string
	mtprotoPort   int
	maskHost      string
	tlsBackend    string
	wildcardMask  string
	mssClamp      bool
	randomPadding bool
	ja4Log        bool
	statsPort     int
	certDir       string
	stubDir       string
	domain        string
	acmeEmail     string
	devMode       bool
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
	serveCmd.Flags().StringVar(&tlsBackend, "tls-backend", "", "TLS backend for invalid Teleproxy handshakes")
	serveCmd.Flags().StringVar(&wildcardMask, "wildcard-mask", "", "wildcard certificate mask for Teleproxy")
	serveCmd.Flags().BoolVar(&mssClamp, "mss-clamp", true, "enable Teleproxy MSS clamp for ClientHello fragmentation")
	serveCmd.Flags().BoolVar(&randomPadding, "random-padding", false, "generate Obfuscated2 (dd) padded links instead of Fake-TLS")
	serveCmd.Flags().BoolVar(&ja4Log, "ja4-log", true, "enable Teleproxy JA4 probe logging")
	serveCmd.Flags().IntVar(&statsPort, "stats-port", 9091, "Teleproxy stats port used when rendering Teleproxy config")
	serveCmd.Flags().StringVar(&certDir, "cert-dir", "/etc/tgproxy/certs", "directory for TLS certificates")
	serveCmd.Flags().StringVar(&stubDir, "stub-dir", "/var/www/tgproxy-stub", "web root directory for stub pages")
	serveCmd.Flags().StringVar(&domain, "domain", "", "panel domain; enables ACME renewal loop when set with --acme-email")
	serveCmd.Flags().StringVar(&acmeEmail, "acme-email", "", "email for Let's Encrypt renewal notifications")
	serveCmd.Flags().BoolVar(&devMode, "dev", false, "run in development mode: in-memory DB, demo data, no system changes")
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
func applyDBSettings(d *db.DB, ret *metrics.Retainer, mtprotoPort *int, maskHost, tlsBackend, wildcardMask *string, mssClamp, randomPadding, ja4Log *bool) {
	if v := d.GetSetting("mask_host", ""); v != "" {
		*maskHost = v
	}
	if v := d.GetSetting("tls_backend", ""); v != "" {
		*tlsBackend = v
	}
	if v := d.GetSetting("wildcard_mask", ""); v != "" {
		*wildcardMask = v
	}
	if v, ok := boolDBSetting(d, "mss_clamp"); ok {
		*mssClamp = v
	}
	if v, ok := boolDBSetting(d, "random_padding"); ok {
		*randomPadding = v
	}
	if v, ok := boolDBSetting(d, "ja4_log"); ok {
		*ja4Log = v
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

func boolDBSetting(d *db.DB, key string) (bool, bool) {
	switch d.GetSetting(key, "") {
	case "1", "true", "on", "yes":
		return true, true
	case "0", "false", "off", "no":
		return false, true
	default:
		return false, false
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	// Dev mode: override defaults before any resource is opened.
	if devMode {
		dbPath = ":memory:"
		if !cmd.Flags().Changed("listen") {
			listenAddr = "127.0.0.1:8080"
		}
		if !cmd.Flags().Changed("path") {
			panelPath = "/"
		}
	}

	d, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer d.Close()

	statsAddr := fmt.Sprintf("http://127.0.0.1:%d", statsPort)
	scraper := metrics.DefaultScraper(statsAddr)
	sampler := metrics.Sampler{
		SnapshotSource: scraper.ScrapeSnapshot,
		Store:          metrics.DBStoreFn(d),
		OpsStore:       metrics.DBOpsStoreFn(d),
		Now:            func() int64 { return time.Now().Unix() },
	}
	retainer := metrics.Retainer{DB: d}
	applyDBSettings(d, &retainer, &mtprotoPort, &maskHost, &tlsBackend, &wildcardMask, &mssClamp, &randomPadding, &ja4Log)

	// Context that is cancelled on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start metrics sampler goroutine (skip in dev mode: no Teleproxy stats endpoint).
	if !devMode {
		go func() {
			if err := sampler.Run(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "metrics sampler: %v\n", err)
			}
		}()
	}

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

	// Start ACME renewal loop when domain and email are configured (not in dev mode).
	if !devMode && domain != "" && acmeEmail != "" {
		mgr := acme.DefaultManager(d, certDir, "")
		if v := d.GetSetting("cert_renew_days", "30"); v != "" {
			if n, e := strconv.Atoi(v); e == nil && n >= 1 && n <= 89 {
				mgr.RenewBeforeDays = n
			}
		}
		mgr.CADirURL = acme.CADirURL(d.GetSetting("cert_acme_provider", "production"))
		webRootDir := filepath.Join(certDir, ".well-known-webroot")
		runner := acme.DefaultRunner(mgr, webRootDir)
		runner.RenewEnabled = func() bool { return d.GetSetting("cert_auto_renew", "1") != "0" }
		certPath := filepath.Join(certDir, domain, "cert.pem")
		runner.StartRenewalLoop(ctx, domain, acmeEmail, certPath, 12*time.Hour)
	}

	srv := &panel.Server{
		DB:          d,
		PanelPath:   panelPath,
		RateLimiter: panel.NewRateLimiter(),
		Secure:      true,
		BridgeCfg: &panel.BridgeConfig{
			MTProtoPort:   mtprotoPort,
			MaskHost:      maskHost,
			TLSBackend:    tlsBackend,
			WildcardMask:  wildcardMask,
			MSSClamp:      mssClamp,
			RandomPadding: randomPadding,
			JA4Log:        ja4Log,
			StatsPort:     statsPort,
		},
		SettingsCfg: &panel.SettingsConfig{
			CertDir:   certDir,
			WebRoot:   stubDir,
			Domain:    domain,
			ServerIP:  d.GetSetting("server_ip", ""),
			ACMEEmail: acmeEmail,
		},
	}

	quotaSvc := quota.NewService(d, func() error { return srv.ReloadTeleproxyForQuota() })
	srv.RecalcUser = func(label string) {
		_, _, _ = quotaSvc.Recalculate(ctx, label)
	}
	go quotaSvc.RunPeriodic(ctx, 5*time.Minute)

	if devMode {
		if err := panel.SeedDevData(d); err != nil {
			return fmt.Errorf("seed dev data: %w", err)
		}
		panel.ApplyDevMode(srv)
		fmt.Fprintf(cmd.OutOrStdout(), "⚠  DEV MODE — in-memory DB, demo data, no system changes\n")
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
