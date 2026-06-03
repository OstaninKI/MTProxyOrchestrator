package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/acme"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/component"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/update"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/version"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:     "tgproxy-cli",
	Short:   "MTProto proxy installer and management tool",
	Version: version.Version,
}

var unattended bool
var panelDomain, panelCert, panelKey, panelEmail string

type preflightRunner interface {
	Run(panelPort int, extraPorts ...int) install.CheckResult
}

var (
	defaultChecker       = func() preflightRunner { return install.DefaultChecker() }
	resolveLocalBinaries = currentLocalBinaries
	buildSinglePlan      = install.BuildSinglePlan
	buildBridgePlan      = install.BuildBridgePlan
	newExecutor          = func() install.Executor { return install.NewSystemExecutor() }
	runPostInstallCheck  = func() error {
		// Allow systemd time to transition services to active state before probing.
		time.Sleep(3 * time.Second)
		checker := health.DefaultChecker()
		result := checker.CheckSingle()
		if !result.OK {
			return fmt.Errorf("health check failed after install:\n%s", formatHealthStatus(result))
		}
		return nil
	}
	statConfigDir = func(path string) error {
		_, err := os.Stat(path)
		return err
	}
	statFile = func(path string) error {
		_, err := os.Stat(path)
		return err
	}
)

var installCmd = &cobra.Command{
	Use:          "install",
	Short:        "Install MTProto proxy (Single or Bridge mode)",
	SilenceUsage: true,
	RunE:         runInstall,
}

func init() {
	installCmd.Flags().BoolVar(&unattended, "unattended", false, "skip interactive prompts, use defaults")
	installCmd.Flags().StringVar(&panelDomain, "panel-domain", "", "domain name for the public HTTPS admin panel")
	installCmd.Flags().StringVar(&panelCert, "panel-cert", "", "TLS certificate path for the public HTTPS admin panel")
	installCmd.Flags().StringVar(&panelKey, "panel-key", "", "TLS private key path for the public HTTPS admin panel")
	installCmd.Flags().StringVar(&panelEmail, "panel-email", "", "email for Let's Encrypt; requires --panel-domain, mutually exclusive with --panel-cert/key")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	cfg := config.Default()
	paths := config.DefaultPaths()
	const panelPort = 8443
	cfg.PanelDomain = panelDomain
	cfg.PanelCertPath = panelCert
	cfg.PanelKeyPath = panelKey
	cfg.ACMEEmail = panelEmail
	installMode := config.ModeSingle
	var firstOutboundURL string
	bridgeStrategy := cfg.BridgeStrategy
	if err := validatePanelSetup(cfg); err != nil {
		return err
	}

	cfgPaths := config.DefaultPaths()
	if err := statConfigDir(cfgPaths.ConfigDir); err == nil {
		return fmt.Errorf("already installed: %s exists — run 'tgproxy-cli uninstall' first", cfgPaths.ConfigDir)
	}

	if result := defaultChecker().Run(panelPort, install.PanelBackendPort); !result.OK() {
		return fmt.Errorf("preflight failed:\n%s", formatCheckResult(result))
	}

	if !unattended {
		p := newPrompter()
		selectedMode, err := p.AskSelect("Install mode", []string{"Single", "Bridge"})
		if err != nil {
			return err
		}
		if strings.EqualFold(selectedMode, "Bridge") {
			installMode = config.ModeBridge
		}

		maskHost, err := p.AskString("Mask host", cfg.MaskHost)
		if err != nil {
			return err
		}
		cfg.MaskHost = maskHost

		if cfg.PanelDomain == "" && cfg.PanelCertPath == "" {
			domain, err := p.AskString("Panel domain for Let's Encrypt (leave empty to skip)", "")
			if err != nil {
				return err
			}
			if domain != "" {
				email, err := p.AskString("Email for Let's Encrypt notifications", "")
				if err != nil {
					return err
				}
				cfg.PanelDomain = domain
				cfg.ACMEEmail = email
			}
		}

		if installMode == config.ModeBridge {
			firstOutboundURL, err = p.AskString("First outbound VLESS Reality share URL", "")
			if err != nil {
				return err
			}
			bridgeStrategy, err = p.AskSelect("Bridge routing strategy", []string{"urltest", "fallback", "selector", "roundrobin"})
			if err != nil {
				return err
			}
			cfg.BridgeStrategy = bridgeStrategy
		}

		ok, err := p.AskConfirm(fmt.Sprintf("Proceed with %s mode install?", installMode), true)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
	}

	binaries, err := resolveLocalBinaries()
	if err != nil {
		return err
	}

	// Obtain Let's Encrypt certificate before building the plan.
	// Uses a standalone HTTP server on port 80 (nginx not yet started).
	if cfg.PanelDomain != "" && cfg.ACMEEmail != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Obtaining Let's Encrypt certificate for %s...\n", cfg.PanelDomain)
		mgr := acme.DefaultManager(nil, paths.CertDir, "")
		runner := acme.DefaultRunner(mgr, paths.CertDir+"/.well-known-webroot")
		certPath, keyPath, err := obtainACMECert(context.Background(), runner, cfg.PanelDomain, cfg.ACMEEmail)
		if err != nil {
			return fmt.Errorf("obtain Let's Encrypt certificate: %w", err)
		}
		cfg.PanelCertPath = certPath
		cfg.PanelKeyPath = keyPath
		fmt.Fprintf(cmd.OutOrStdout(), "Certificate obtained: %s\n", certPath)
	}

	var plan install.Plan
	if installMode == config.ModeBridge {
		plan, err = buildBridgePlan(cfg, paths, panelPort, binaries, firstOutboundURL, bridgeStrategy)
	} else {
		plan, err = buildSinglePlan(cfg, paths, panelPort, binaries)
	}
	if err != nil {
		return fmt.Errorf("build plan: %w", err)
	}

	exec := newExecutor()
	inst := install.Installer{Executor: exec, Plan: plan}
	if err := inst.Run(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Installation complete. Verifying services...")
	if err := runPostInstallCheck(); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Install succeeded but health check failed. Stopping services.")
		// Stop panel before teleproxy: panel depends on teleproxy being available.
		stopServiceFn("tgproxy-panel.service")
		stopServiceFn("teleproxy.service")
		fmt.Fprintln(cmd.OutOrStdout(), "\nGenerated access details (valid after services are fixed):")
		printInstallAccess(cmd, cfg, panelPort, plan)
		fmt.Fprintln(cmd.ErrOrStderr(), "Inspect teleproxy with: journalctl -u teleproxy.service -n 120 --no-pager")
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Services healthy.")
	fmt.Fprintln(cmd.OutOrStdout(), "\nGenerated access details:")
	printInstallAccess(cmd, cfg, panelPort, plan)
	return nil
}

func printInstallAccess(cmd *cobra.Command, cfg config.Config, panelPort int, plan install.Plan) {
	fmt.Fprintf(cmd.OutOrStdout(), "Panel URL:  https://%s:%d%s\n", displayPanelHost(cfg.PanelDomain), panelPort, plan.Creds.PanelPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Login:      %s\n", plan.Creds.AdminLogin)
	fmt.Fprintf(cmd.OutOrStdout(), "Password:   %s\n", plan.Creds.AdminPassword)
	fmt.Fprintf(cmd.OutOrStdout(), "First user: tg://proxy?server=<your-ip>&port=443&secret=%s\n", plan.Creds.FirstUser.Secret.Hex())
}

func displayPanelHost(domain string) string {
	if strings.TrimSpace(domain) != "" {
		return domain
	}
	return "<your-server-ip>"
}

// validatePanelSetup ensures the panel TLS/ACME flags are used correctly:
//   - manual mode: --panel-domain, --panel-cert, --panel-key all together
//   - ACME mode:   --panel-domain + --panel-email, no --panel-cert/key
//   - no panel:    all flags empty
func validatePanelSetup(cfg config.Config) error {
	hasManual := strings.TrimSpace(cfg.PanelCertPath) != "" || strings.TrimSpace(cfg.PanelKeyPath) != ""
	hasACME := strings.TrimSpace(cfg.ACMEEmail) != ""
	hasDomain := strings.TrimSpace(cfg.PanelDomain) != ""

	if hasManual && hasACME {
		return fmt.Errorf("--panel-email and --panel-cert/key are mutually exclusive")
	}
	if hasManual {
		// All three manual flags required together.
		set := 0
		for _, v := range []string{cfg.PanelDomain, cfg.PanelCertPath, cfg.PanelKeyPath} {
			if strings.TrimSpace(v) != "" {
				set++
			}
		}
		if set != 3 {
			return fmt.Errorf("panel-domain, panel-cert, and panel-key must be provided together")
		}
		if err := statFile(cfg.PanelCertPath); err != nil {
			return fmt.Errorf("panel-cert file not found: %s", cfg.PanelCertPath)
		}
		if err := statFile(cfg.PanelKeyPath); err != nil {
			return fmt.Errorf("panel-key file not found: %s", cfg.PanelKeyPath)
		}
	}
	if hasACME && !hasDomain {
		return fmt.Errorf("--panel-email requires --panel-domain")
	}
	return nil
}

// obtainACMECert is a variable so tests can replace it without a real ACME server.
var obtainACMECert = func(ctx context.Context, runner *acme.Runner, domain, email string) (certPath, keyPath string, err error) {
	return runner.ObtainCert(ctx, domain, email)
}

func currentLocalBinaries() (install.LocalBinaries, error) {
	cliPath, err := os.Executable()
	if err != nil {
		return install.LocalBinaries{}, fmt.Errorf("resolve current executable: %w", err)
	}
	return siblingLocalBinaries(cliPath)
}

func siblingLocalBinaries(cliPath string) (install.LocalBinaries, error) {
	panelPath := filepath.Join(filepath.Dir(cliPath), "tgproxy-panel")
	if _, err := os.Stat(panelPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if downloadErr := downloadPanelBinary(panelPath); downloadErr != nil {
				return install.LocalBinaries{}, fmt.Errorf("tgproxy-panel not found next to %s and could not be downloaded automatically; place the binary at %s manually: %w", cliPath, panelPath, downloadErr)
			}
		} else {
			return install.LocalBinaries{}, fmt.Errorf("stat tgproxy-panel at %s: %w", panelPath, err)
		}
	}
	return install.LocalBinaries{
		CLI:   cliPath,
		Panel: panelPath,
	}, nil
}

// downloadPanelBinary fetches tgproxy-panel pinned to the running CLI's own
// version into destPath, verifying its SHA256 against the release checksums.
var downloadPanelBinary = func(destPath string) error {
	ver := version.Version
	if strings.TrimSpace(ver) == "" || ver == "dev" {
		return fmt.Errorf("cannot auto-download tgproxy-panel for a development build (version %q); place the binary next to tgproxy-cli manually", ver)
	}
	url, sha, err := update.ResolvePinnedAsset(nil, update.ComponentPanel, ver)
	if err != nil {
		return fmt.Errorf("resolve tgproxy-panel release asset: %w", err)
	}
	fmt.Fprintf(os.Stderr, "tgproxy-panel not found locally; downloading v%s ...\n", ver)
	if err := (component.Downloader{}).Download(url, sha, destPath); err != nil {
		return fmt.Errorf("download tgproxy-panel: %w", err)
	}
	return nil
}

func formatCheckResult(result install.CheckResult) string {
	lines := make([]string, 0, len(result.Errors))
	for _, checkErr := range result.Errors {
		lines = append(lines, fmt.Sprintf("- %s: %s (%s)", checkErr.Check, checkErr.Description, checkErr.Remediation))
	}
	return strings.Join(lines, "\n")
}

func formatHealthStatus(result health.Status) string {
	lines := make([]string, 0, len(result.Services))
	for _, svc := range result.Services {
		state := "ok"
		if !svc.Active {
			state = "down"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (%s)", svc.Name, state, svc.Message))
	}
	return strings.Join(lines, "\n")
}
