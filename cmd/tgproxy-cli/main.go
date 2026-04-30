package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

type preflightRunner interface {
	Run(panelPort int) install.CheckResult
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
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	cfg := config.Default()
	paths := config.DefaultPaths()
	const panelPort = 8443

	if result := defaultChecker().Run(panelPort); !result.OK() {
		return fmt.Errorf("preflight failed:\n%s", formatCheckResult(result))
	}

	if !unattended {
		p := install.NewHuhPrompter()
		maskHost, err := p.AskString("Mask host", cfg.MaskHost)
		if err != nil {
			return err
		}
		cfg.MaskHost = maskHost

		ok, err := p.AskConfirm("Proceed with Single mode install?", true)
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

	plan, err := buildSinglePlan(cfg, paths, panelPort, binaries)
	if err != nil {
		return fmt.Errorf("build plan: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Panel URL:  https://<your-domain>%s\n", plan.Creds.PanelPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Login:      %s\n", plan.Creds.AdminLogin)
	fmt.Fprintf(cmd.OutOrStdout(), "Password:   %s\n", plan.Creds.AdminPassword)
	fmt.Fprintf(cmd.OutOrStdout(), "First user: tg://proxy?server=<your-ip>&port=443&secret=%s\n", plan.Creds.FirstUser.Secret.Hex())

	exec := newExecutor()
	inst := install.Installer{Executor: exec, Plan: plan}
	return inst.Run()
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
