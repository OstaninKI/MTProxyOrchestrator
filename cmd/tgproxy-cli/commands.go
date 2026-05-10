package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/backup"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/health"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/secrets"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/teleproxy"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/update"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(resetAdminCmd)
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(restoreCmd)

	updateCmd.Flags().BoolVar(&updateManual, "manual", true, "bypass the 18-hour rate limit")
	backupCmd.Flags().StringVar(&backupDest, "dest", "", "destination archive path (required)")
	backupCmd.Flags().StringVar(&backupPass, "passphrase", "", "encryption passphrase (required)")
	_ = backupCmd.MarkFlagRequired("dest")
	_ = backupCmd.MarkFlagRequired("passphrase")
	restoreCmd.Flags().StringVar(&restorePass, "passphrase", "", "decryption passphrase (required)")
	_ = restoreCmd.MarkFlagRequired("passphrase")
}

var updateManual bool
var backupDest, backupPass string
var restorePass string

var (
	newPrompter    = func() install.Prompter { return install.NewHuhPrompter() }
	defaultPaths   = config.DefaultPaths
	restoreArchive = backup.Restore
	stopServiceFn  = stopService
	startServiceFn = startService
	newChecker     = health.DefaultChecker
)

// statusCmd prints service health for the current mode.
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show service health",
	RunE: func(cmd *cobra.Command, args []string) error {
		checker := newChecker()
		mode := currentRuntimeMode(defaultPaths())
		var st health.Status
		if mode == config.ModeBridge {
			bridgeStatus := checker.CheckBridge()
			st = health.Status{
				OK:      bridgeStatus.OK,
				Summary: bridgeStatus.Summary,
			}
			for _, step := range bridgeStatus.Steps {
				st.Services = append(st.Services, health.ServiceState{
					Name:    step.Name,
					Active:  step.OK,
					Message: step.Message,
				})
			}
		} else {
			st = checker.CheckSingle()
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		for _, svc := range st.Services {
			state := "active"
			if !svc.Active {
				state = "inactive"
			}
			fmt.Fprintf(w, "%s\t%s\n", svc.Name, state)
		}
		_ = w.Flush()

		if !st.OK {
			fmt.Fprintln(cmd.OutOrStdout(), "\nStatus: DEGRADED")
			return fmt.Errorf("one or more services are not healthy")
		}
		fmt.Fprintln(cmd.OutOrStdout(), "\nStatus: OK")
		return nil
	},
}

// updateCmd checks for and applies available component updates.
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check and apply component updates",
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := defaultPaths()

		currentVersions := map[update.Component]string{
			update.ComponentCLI:       version.Version,
			update.ComponentPanel:     version.Version,
			update.ComponentTeleproxy: installedVersion(paths.TeleproxyBin),
			update.ComponentSingbox:   installedVersion(paths.SingboxBin),
		}

		checker, err := update.NewChecker(paths.ConfigDir, currentVersions)
		if err != nil {
			return fmt.Errorf("init update checker: %w", err)
		}

		updates, err := checker.CheckAll(updateManual)
		if err != nil {
			return fmt.Errorf("check updates: %w", err)
		}

		if len(updates) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "All components are up to date.")
			return nil
		}

		for _, u := range updates {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", u.Component, u.CurrentVersion, u.AvailableVersion)
			if u.DownloadURL == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  (no download URL found for this platform, skipping)\n")
				continue
			}
			if u.SHA256 == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  (no SHA256 checksum available, skipping for safety)\n")
				continue
			}
			destPath := componentDestPath(paths, u.Component)
			if destPath == "" {
				continue
			}
			applier := update.NewDefaultApplier()
			if applyErr := applier.Apply(u, destPath); applyErr != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "  update failed: %v\n", applyErr)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "  updated successfully\n")
			}
		}
		return nil
	},
}

// uninstallCmd stops services and removes installed files.
var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove MTProto proxy and all installed files",
	RunE: func(cmd *cobra.Command, args []string) error {
		p := newPrompter()
		ok, err := p.AskConfirm("This will stop services and remove all installed files. Continue?", false)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}

		paths := config.DefaultPaths()

		services := []string{"tgproxy-panel.service", "teleproxy.service", "sing-box.service"}
		for _, svc := range services {
			stopService(svc)
			disableService(svc)
		}

		removeFiles := []string{
			paths.CLIBin,
			paths.PanelBin,
			paths.TeleproxyBin,
			paths.SingboxBin,
			paths.TeleproxyService,
			paths.SingboxService,
			paths.PanelService,
			"/etc/nginx/sites-enabled/tgproxy-stub",
			"/etc/nginx/sites-available/tgproxy-stub",
		}
		for _, f := range removeFiles {
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: remove %s: %v\n", f, err)
			}
		}

		removeDirs := []string{
			paths.ConfigDir,
			paths.LogDir,
			paths.StubDir,
		}
		for _, d := range removeDirs {
			if err := os.RemoveAll(d); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: remove %s: %v\n", d, err)
			}
		}

		if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: daemon-reload: %v\n", err)
		}
		if err := exec.Command("nginx", "-s", "reload").Run(); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: nginx reload: %v\n", err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), "Uninstall complete.")
		return nil
	},
}

// resetAdminCmd resets the admin password in the panel DB.
var resetAdminCmd = &cobra.Command{
	Use:   "reset-admin-password",
	Short: "Reset the admin panel password",
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := defaultPaths()

		newLogin, err := secrets.GenerateAdminLogin()
		if err != nil {
			return fmt.Errorf("generate login: %w", err)
		}
		newPassword, err := secrets.GenerateAdminPassword()
		if err != nil {
			return fmt.Errorf("generate password: %w", err)
		}

		hash, err := panel.HashPassword(newPassword)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}

		database, err := db.Open(paths.PanelDB)
		if err != nil {
			return fmt.Errorf("open panel db: %w", err)
		}
		defer database.Close()

		if _, err := database.Exec(
			`UPDATE admin SET login=?, password_hash=?, updated_at=datetime('now') WHERE id=1`,
			newLogin, hash,
		); err != nil {
			return fmt.Errorf("update admin credentials: %w", err)
		}
		if _, err := database.Exec(`DELETE FROM sessions`); err != nil {
			return fmt.Errorf("invalidate sessions: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "New login:    %s\n", newLogin)
		fmt.Fprintf(cmd.OutOrStdout(), "New password: %s\n", newPassword)
		fmt.Fprintln(cmd.OutOrStdout(), "Restart tgproxy-panel for the change to take effect.")
		return nil
	},
}

// backupCmd creates an encrypted backup archive.
var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Create an encrypted backup of /etc/tgproxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := config.DefaultPaths()

		opts := backup.BackupOptions{
			ConfigDir:  paths.ConfigDir,
			PanelDB:    paths.PanelDB,
			Passphrase: backupPass,
			DestPath:   backupDest,
		}
		if err := backup.Create(opts); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Backup written to %s\n", backupDest)
		return nil
	},
}

// restoreCmd decrypts and restores a backup archive.
var restoreCmd = &cobra.Command{
	Use:   "restore <archive>",
	Short: "Restore /etc/tgproxy from an encrypted backup",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := defaultPaths()

		p := newPrompter()
		ok, err := p.AskConfirm("This will overwrite /etc/tgproxy. Continue?", false)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}

		previousNeedsSingbox := restoreNeedsSingbox(paths.TeleproxyTOML)

		stopServiceFn("tgproxy-panel.service")
		stopServiceFn("teleproxy.service")
		stopServiceFn("sing-box.service")

		opts := backup.RestoreOptions{
			ArchivePath: args[0],
			TargetDir:   paths.ConfigDir,
			Passphrase:  restorePass,
		}
		if err := restoreArchive(opts); err != nil {
			if previousNeedsSingbox {
				startServiceFn("sing-box.service")
			}
			startServiceFn("teleproxy.service")
			startServiceFn("tgproxy-panel.service")
			return fmt.Errorf("restore failed: %w", err)
		}

		if restoreNeedsSingbox(paths.TeleproxyTOML) {
			startServiceFn("sing-box.service")
		}
		startServiceFn("teleproxy.service")
		startServiceFn("tgproxy-panel.service")

		fmt.Fprintln(cmd.OutOrStdout(), "Restore complete. Services restarted.")
		return nil
	},
}

// helpers

func stopService(svc string) {
	_ = exec.Command("systemctl", "stop", svc).Run()
}

func disableService(svc string) {
	_ = exec.Command("systemctl", "disable", svc).Run()
}

func startService(svc string) {
	_ = exec.Command("systemctl", "start", svc).Run()
}

func restoreNeedsSingbox(teleproxyConfigPath string) bool {
	mode, err := teleproxy.DetectMode(teleproxyConfigPath)
	return err == nil && mode == config.ModeBridge
}

func currentRuntimeMode(paths config.InstallPaths) config.Mode {
	mode, err := teleproxy.DetectMode(paths.TeleproxyTOML)
	if err != nil {
		return config.ModeSingle
	}
	return mode
}

func installedVersion(binPath string) string {
	out, err := exec.Command(binPath, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func componentDestPath(paths config.InstallPaths, comp update.Component) string {
	switch comp {
	case update.ComponentCLI:
		return paths.CLIBin
	case update.ComponentPanel:
		return paths.PanelBin
	case update.ComponentTeleproxy:
		return paths.TeleproxyBin
	case update.ComponentSingbox:
		return paths.SingboxBin
	default:
		return ""
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
