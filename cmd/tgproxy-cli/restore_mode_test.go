package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/backup"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
)

type confirmPrompter struct{}

func (confirmPrompter) AskString(string, string) (string, error)   { return "", nil }
func (confirmPrompter) AskSelect(string, []string) (string, error) { return "", nil }
func (confirmPrompter) AskConfirm(string, bool) (bool, error)      { return true, nil }

func TestRestoreStartsSingboxWhenBridgeStateExists(t *testing.T) {
	oldNewPrompter := newPrompter
	oldStopService := stopServiceFn
	oldStartService := startServiceFn
	oldRestore := restoreArchive
	oldPaths := defaultPaths
	oldPass := restorePass
	t.Cleanup(func() {
		newPrompter = oldNewPrompter
		stopServiceFn = oldStopService
		startServiceFn = oldStartService
		restoreArchive = oldRestore
		defaultPaths = oldPaths
		restorePass = oldPass
	})

	dir := t.TempDir()
	paths := config.DefaultPaths()
	paths.ConfigDir = dir
	paths.TeleproxyTOML = filepath.Join(dir, "teleproxy.toml")
	paths.SingboxJSON = filepath.Join(dir, "sing-box.json")
	paths.OutboundsJSON = filepath.Join(dir, "nodes", "outbounds.json")
	defaultPaths = func() config.InstallPaths { return paths }

	newPrompter = func() install.Prompter { return confirmPrompter{} }
	var stopped []string
	var started []string
	stopServiceFn = func(s string) { stopped = append(stopped, s) }
	startServiceFn = func(s string) { started = append(started, s) }
	restoreArchive = func(opts backup.RestoreOptions) error {
		if err := os.MkdirAll(filepath.Dir(paths.TeleproxyTOML), 0o700); err != nil {
			return err
		}
		return os.WriteFile(paths.TeleproxyTOML, []byte("port = 443\nsocks5 = \"127.0.0.1:1080\"\n"), 0o600)
	}
	restorePass = "secret"

	if err := restoreCmd.RunE(restoreCmd, []string{"/tmp/archive.enc"}); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(stopped, "sing-box.service") {
		t.Fatalf("expected sing-box.service to be stopped before restore, got %v", stopped)
	}
	if !slices.Contains(started, "sing-box.service") {
		t.Fatalf("expected sing-box.service to be started after bridge restore, got %v", started)
	}
}

func TestRestoreSkipsSingboxWhenBridgeStateAbsent(t *testing.T) {
	oldNewPrompter := newPrompter
	oldStopService := stopServiceFn
	oldStartService := startServiceFn
	oldRestore := restoreArchive
	oldPaths := defaultPaths
	oldPass := restorePass
	t.Cleanup(func() {
		newPrompter = oldNewPrompter
		stopServiceFn = oldStopService
		startServiceFn = oldStartService
		restoreArchive = oldRestore
		defaultPaths = oldPaths
		restorePass = oldPass
	})

	dir := t.TempDir()
	paths := config.DefaultPaths()
	paths.ConfigDir = dir
	paths.TeleproxyTOML = filepath.Join(dir, "teleproxy.toml")
	paths.SingboxJSON = filepath.Join(dir, "sing-box.json")
	paths.OutboundsJSON = filepath.Join(dir, "nodes", "outbounds.json")
	defaultPaths = func() config.InstallPaths { return paths }

	newPrompter = func() install.Prompter { return confirmPrompter{} }
	var started []string
	stopServiceFn = func(string) {}
	startServiceFn = func(s string) { started = append(started, s) }
	restoreArchive = func(opts backup.RestoreOptions) error { return nil }
	restorePass = "secret"

	if err := restoreCmd.RunE(restoreCmd, []string{"/tmp/archive.enc"}); err != nil {
		t.Fatal(err)
	}

	if slices.Contains(started, "sing-box.service") {
		t.Fatalf("did not expect sing-box.service start without bridge state, got %v", started)
	}
}

func TestRestoreRestartsPreviousSingleServicesAfterFailedRestore(t *testing.T) {
	oldNewPrompter := newPrompter
	oldStopService := stopServiceFn
	oldStartService := startServiceFn
	oldRestore := restoreArchive
	oldPaths := defaultPaths
	oldPass := restorePass
	t.Cleanup(func() {
		newPrompter = oldNewPrompter
		stopServiceFn = oldStopService
		startServiceFn = oldStartService
		restoreArchive = oldRestore
		defaultPaths = oldPaths
		restorePass = oldPass
	})

	dir := t.TempDir()
	paths := config.DefaultPaths()
	paths.ConfigDir = dir
	paths.TeleproxyTOML = filepath.Join(dir, "teleproxy.toml")
	defaultPaths = func() config.InstallPaths { return paths }
	if err := os.WriteFile(paths.TeleproxyTOML, []byte("port = 443\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	newPrompter = func() install.Prompter { return confirmPrompter{} }
	var events []string
	stopServiceFn = func(s string) { events = append(events, "stop:"+s) }
	startServiceFn = func(s string) { events = append(events, "start:"+s) }
	restoreArchive = func(opts backup.RestoreOptions) error {
		return errors.New("restore archive failed")
	}
	restorePass = "secret"

	err := restoreCmd.RunE(restoreCmd, []string{"/tmp/archive.enc"})
	if err == nil {
		t.Fatal("expected restore error")
	}

	want := []string{
		"stop:tgproxy-panel.service",
		"stop:teleproxy.service",
		"stop:sing-box.service",
		"start:teleproxy.service",
		"start:tgproxy-panel.service",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestRestoreRestartsPreviousBridgeServicesAfterFailedRestore(t *testing.T) {
	oldNewPrompter := newPrompter
	oldStopService := stopServiceFn
	oldStartService := startServiceFn
	oldRestore := restoreArchive
	oldPaths := defaultPaths
	oldPass := restorePass
	t.Cleanup(func() {
		newPrompter = oldNewPrompter
		stopServiceFn = oldStopService
		startServiceFn = oldStartService
		restoreArchive = oldRestore
		defaultPaths = oldPaths
		restorePass = oldPass
	})

	dir := t.TempDir()
	paths := config.DefaultPaths()
	paths.ConfigDir = dir
	paths.TeleproxyTOML = filepath.Join(dir, "teleproxy.toml")
	defaultPaths = func() config.InstallPaths { return paths }
	if err := os.WriteFile(paths.TeleproxyTOML, []byte("port = 443\nsocks5 = \"127.0.0.1:1080\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	newPrompter = func() install.Prompter { return confirmPrompter{} }
	var events []string
	stopServiceFn = func(s string) { events = append(events, "stop:"+s) }
	startServiceFn = func(s string) { events = append(events, "start:"+s) }
	restoreArchive = func(opts backup.RestoreOptions) error {
		if err := os.WriteFile(paths.TeleproxyTOML, []byte("port = 443\n"), 0o600); err != nil {
			return err
		}
		return errors.New("restore archive failed")
	}
	restorePass = "secret"

	err := restoreCmd.RunE(restoreCmd, []string{"/tmp/archive.enc"})
	if err == nil {
		t.Fatal("expected restore error")
	}

	want := []string{
		"stop:tgproxy-panel.service",
		"stop:teleproxy.service",
		"stop:sing-box.service",
		"start:sing-box.service",
		"start:teleproxy.service",
		"start:tgproxy-panel.service",
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}
