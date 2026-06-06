package db_test

import (
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

func TestOpenInMemory(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
}

func TestMigrationsIdempotent(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 9 {
		t.Errorf("expected 9 migrations recorded, got %d", count)
	}
}

func TestSchemaTablesExist(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tables := []string{"admin", "sessions", "users", "audit_log", "migrations", "settings", "traffic_counters"}
	for _, tbl := range tables {
		var name string
		err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", tbl, err)
		}
	}
}

func TestGetSetSetting(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// default when absent
	got := d.GetSetting("mask_host", "default.example.com")
	if got != "default.example.com" {
		t.Fatalf("want default, got %q", got)
	}

	// set and get
	if err := d.SetSetting("mask_host", "www.google.com"); err != nil {
		t.Fatal(err)
	}
	got = d.GetSetting("mask_host", "default.example.com")
	if got != "www.google.com" {
		t.Fatalf("want www.google.com, got %q", got)
	}

	// overwrite
	if err := d.SetSetting("mask_host", "www.apple.com"); err != nil {
		t.Fatal(err)
	}
	got = d.GetSetting("mask_host", "default.example.com")
	if got != "www.apple.com" {
		t.Fatalf("want www.apple.com, got %q", got)
	}
}
