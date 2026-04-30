package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

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
