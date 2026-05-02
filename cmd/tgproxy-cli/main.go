package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/acme"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
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
	newExecutor          = func() install.Executor { return install.NewSystemExecutor() }
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install MTProto proxy (Single or Bridge mode)",
	RunE:  runInstall,
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
	if err := validatePanelSetup(cfg); err != nil {
		return err
	}

	if result := defaultChecker().Run(panelPort, install.PanelBackendPort); !result.OK() {
		return fmt.Errorf("preflight failed:\n%s", formatCheckResult(result))
	}

	if !unattended {
		p := install.NewHuhPrompter()
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

		ok, err := p.AskConfirm("Proceed with Single mode install?", true)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
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

	binaries, err := resolveLocalBinaries()
	if err != nil {
		return err
	}

	plan, err := buildSinglePlan(cfg, paths, panelPort, binaries)
	if err != nil {
		return fmt.Errorf("build plan: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Panel URL:  https://%s:%d%s\n", cfg.PanelDomain, panelPort, plan.Creds.PanelPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Login:      %s\n", plan.Creds.AdminLogin)
	fmt.Fprintf(cmd.OutOrStdout(), "Password:   %s\n", plan.Creds.AdminPassword)
	fmt.Fprintf(cmd.OutOrStdout(), "First user: tg://proxy?server=<your-ip>&port=443&secret=%s\n", plan.Creds.FirstUser.Secret.Hex())

	exec := newExecutor()
	inst := install.Installer{Executor: exec, Plan: plan}
	return inst.Run()
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
	}
	if hasACME && !hasDomain {
		return fmt.Errorf("--panel-email requires --panel-domain")
	}
	return nil
}

// obtainACMECert is a variable so tests can replace it without a real ACME server.
var obtainACMECert = func(ctx context.Context, runner acme.Runner, domain, email string) (certPath, keyPath string, err error) {
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
		return install.LocalBinaries{}, fmt.Errorf("resolve tgproxy-panel рядом с %s: expected %s: %w", cliPath, panelPath, err)
	}
	return install.LocalBinaries{
		CLI:   cliPath,
		Panel: panelPath,
	}, nil
}

func formatCheckResult(result install.CheckResult) string {
	lines := make([]string, 0, len(result.Errors))
	for _, checkErr := range result.Errors {
		lines = append(lines, fmt.Sprintf("- %s: %s (%s)", checkErr.Check, checkErr.Description, checkErr.Remediation))
	}
	return strings.Join(lines, "\n")
}
