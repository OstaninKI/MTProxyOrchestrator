package main

import (
	"fmt"
	"os"

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

	plan, err := install.BuildSinglePlan(cfg, paths, panelPort)
	if err != nil {
		return fmt.Errorf("build plan: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Panel URL:  https://<your-domain>%s\n", plan.Creds.PanelPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Login:      %s\n", plan.Creds.AdminLogin)
	fmt.Fprintf(cmd.OutOrStdout(), "Password:   %s\n", plan.Creds.AdminPassword)
	fmt.Fprintf(cmd.OutOrStdout(), "First user: tg://proxy?server=<your-ip>&port=443&secret=%s\n", plan.Creds.FirstUser.Secret.Hex())

	exec := install.NewSystemExecutor()
	inst := install.Installer{Executor: exec, Plan: plan}
	return inst.Run()
}
