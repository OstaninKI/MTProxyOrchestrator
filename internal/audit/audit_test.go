package audit_test

import (
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

func TestLogWritesRow(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := audit.Log(d, 0, "login", "admin", "", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='login'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 audit row, got %d", count)
	}
}

func TestLogSystemAction(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := audit.Log(d, 0, "startup", "", "", ""); err != nil {
		t.Fatal(err)
	}

	var adminID interface{}
	if err := d.QueryRow(`SELECT admin_id FROM audit_log WHERE action='startup'`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if adminID != nil {
		t.Errorf("system action admin_id should be NULL, got %v", adminID)
	}
}
