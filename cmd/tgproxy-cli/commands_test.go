package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
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
