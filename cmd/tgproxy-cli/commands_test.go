package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
	"github.com/spf13/cobra"
)

// captureOutput runs a Cobra command and returns stdout as a string.
func captureOutput(t *testing.T, cmd *cobra.Command, args []string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	_, err := cmd.ExecuteC()
	return buf.String(), err
}

func TestStatusCmd_RegistratedInRoot(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "status" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("status command not registered in rootCmd")
	}
}

func TestStatusCmdDoesNotPrintUsageForRuntimeErrors(t *testing.T) {
	if !statusCmd.SilenceUsage {
		t.Fatal("status command must suppress usage output for runtime failures")
	}
}

func TestUpdateCmd_RegistratedInRoot(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "update" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("update command not registered in rootCmd")
	}
}

func TestBackupCmd_RequiresFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "tgproxy-cli"}
	bc := *backupCmd
	cmd.AddCommand(&bc)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"backup"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when required flags are missing")
	}
}

func TestRestoreCmd_RequiresPassphrase(t *testing.T) {
	cmd := &cobra.Command{Use: "tgproxy-cli"}
	rc := *restoreCmd
	cmd.AddCommand(&rc)

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"restore", "/tmp/archive.enc"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when passphrase flag is missing")
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d        time.Duration
		contains string
	}{
		{30 * time.Second, "s"},
		{5 * time.Minute, "m"},
		{3 * time.Hour, "h"},
	}
	for _, c := range cases {
		got := formatDuration(c.d)
		if !strings.Contains(got, c.contains) {
			t.Errorf("formatDuration(%v) = %q, want suffix %q", c.d, got, c.contains)
		}
	}
}

func TestResetAdminPasswordInvalidatesExistingSessions(t *testing.T) {
	oldPaths := defaultPaths
	t.Cleanup(func() { defaultPaths = oldPaths })

	dir := t.TempDir()
	paths := config.DefaultPaths()
	paths.PanelDB = dir + "/panel.db"
	defaultPaths = func() config.InstallPaths { return paths }
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open(paths.PanelDB)
	if err != nil {
		t.Fatal(err)
	}

	hash, err := panel.HashPassword("old-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO admin(id, login, password_hash) VALUES(1, ?, ?)`, "admin", hash); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO sessions(id, admin_id, expires_at, ip) VALUES('session-1', 1, datetime('now', '+1 day'), '127.0.0.1')`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	resetAdminCmd.SetOut(&buf)
	resetAdminCmd.SetErr(&buf)
	if err := resetAdminCmd.RunE(resetAdminCmd, nil); err != nil {
		t.Fatal(err)
	}

	database, err = db.Open(paths.PanelDB)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var sessions int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("expected all sessions to be invalidated, got %d active sessions", sessions)
	}
	if out := buf.String(); !strings.Contains(out, "New login:") || !strings.Contains(out, "New password:") {
		t.Fatalf("expected reset command to print new credentials, got %q", out)
	}
}

func TestResetTOTPClearsAdminTOTPColumns(t *testing.T) {
	oldPaths := defaultPaths
	t.Cleanup(func() { defaultPaths = oldPaths })

	dir := t.TempDir()
	paths := config.DefaultPaths()
	paths.PanelDB = dir + "/panel.db"
	defaultPaths = func() config.InstallPaths { return paths }
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open(paths.PanelDB)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := panel.HashPassword("old-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO admin(id, login, password_hash, totp_secret, totp_enabled, totp_recovery_codes) VALUES(1, ?, ?, 'JBSWY3DPEHPK3PXP', 1, 'code1,code2')`,
		"admin", hash,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	resetTOTPYes = true
	t.Cleanup(func() { resetTOTPYes = false })

	var buf bytes.Buffer
	resetTOTPCmd.SetOut(&buf)
	resetTOTPCmd.SetErr(&buf)
	if err := resetTOTPCmd.RunE(resetTOTPCmd, nil); err != nil {
		t.Fatal(err)
	}

	database, err = db.Open(paths.PanelDB)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var enabled int
	var secret, recovery string
	if err := database.QueryRow(`SELECT totp_enabled, totp_secret, totp_recovery_codes FROM admin WHERE id=1`).Scan(&enabled, &secret, &recovery); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 || secret != "" || recovery != "" {
		t.Fatalf("expected totp columns cleared, got enabled=%d secret=%q recovery=%q", enabled, secret, recovery)
	}
	if out := buf.String(); !strings.Contains(out, "disabled") {
		t.Fatalf("expected confirmation message, got %q", out)
	}
}

func TestCurrentRuntimeModeReadsTeleproxyConfig(t *testing.T) {
	dir := t.TempDir()
	teleproxyPath := dir + "/teleproxy.toml"
	if err := os.WriteFile(teleproxyPath, []byte("port = 443\nsocks5 = \"127.0.0.1:1080\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	mode := currentRuntimeMode(config.InstallPaths{TeleproxyTOML: teleproxyPath})
	if mode != config.ModeBridge {
		t.Fatalf("mode = %q, want %q", mode, config.ModeBridge)
	}
}

type stubPrompter struct {
	confirmResult bool
}

func (s *stubPrompter) AskString(label, defaultVal string) (string, error) {
	return defaultVal, nil
}

func (s *stubPrompter) AskSelect(label string, options []string) (string, error) {
	if len(options) > 0 {
		return options[0], nil
	}
	return "", nil
}

func (s *stubPrompter) AskConfirm(prompt string, defaultVal bool) (bool, error) {
	return s.confirmResult, nil
}

func TestUninstallWarnsOnRemoveFailure(t *testing.T) {
	// Create a temp file, then make its parent dir read-only so removal fails
	tmp := t.TempDir()
	targetFile := filepath.Join(tmp, "tgproxy-cli")
	if err := os.WriteFile(targetFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// Make the directory read-only so os.Remove fails with permission error
	if err := os.Chmod(tmp, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(tmp, 0755) })

	// Replace defaultPaths to return our test file
	orig := defaultPaths
	defaultPaths = func() config.InstallPaths {
		p := config.DefaultPaths()
		p.CLIBin = targetFile
		p.ConfigDir = filepath.Join(tmp, "etc", "tgproxy")
		p.LogDir = filepath.Join(tmp, "var", "log", "tgproxy")
		p.StubDir = filepath.Join(tmp, "var", "www", "stub")
		return p
	}
	t.Cleanup(func() { defaultPaths = orig })

	// Stub service stop and disable
	origStop := stopServiceFn
	stopServiceFn = func(string) {}
	t.Cleanup(func() { stopServiceFn = origStop })

	// Stub prompter to always confirm
	origPrompter := newPrompter
	newPrompter = func() install.Prompter { return &stubPrompter{confirmResult: true} }
	t.Cleanup(func() { newPrompter = origPrompter })

	var stderr strings.Builder
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(&stderr)
	err := uninstallCmd.RunE(cmd, nil)
	if err != nil {
		t.Fatalf("uninstall must succeed even on remove errors, got %v", err)
	}

	output := stderr.String()
	if !strings.Contains(output, "warning:") {
		t.Fatalf("expected warning in stderr, got %q", output)
	}
}
