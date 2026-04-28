package panel_test

import (
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
)

func TestHashPasswordAndCheck(t *testing.T) {
	hash, err := panel.HashPassword("s3cr3tpassword!")
	if err != nil {
		t.Fatal(err)
	}
	if !panel.CheckPassword(hash, "s3cr3tpassword!") {
		t.Error("correct password should pass check")
	}
	if panel.CheckPassword(hash, "wrongpassword") {
		t.Error("wrong password should fail check")
	}
}

func TestHashPasswordUseBcrypt(t *testing.T) {
	hash, err := panel.HashPassword("test")
	if err != nil {
		t.Fatal(err)
	}
	// bcrypt hashes start with $2a$ or $2b$
	if len(hash) < 10 || (hash[:3] != "$2a" && hash[:3] != "$2b") {
		t.Errorf("expected bcrypt hash, got: %s", hash)
	}
}

func TestNewSessionIDLength(t *testing.T) {
	id, err := panel.NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 64 {
		t.Errorf("session ID length = %d, want 64", len(id))
	}
}

func TestNewSessionIDUniqueness(t *testing.T) {
	a, err := panel.NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := panel.NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two session IDs are identical")
	}
}

func TestSessionExpiry(t *testing.T) {
	before := time.Now()
	exp := panel.SessionExpiry()
	after := time.Now()
	if exp.Before(before.Add(23 * time.Hour)) {
		t.Error("expiry should be at least 23h from now")
	}
	if exp.After(after.Add(25 * time.Hour)) {
		t.Error("expiry should not exceed 25h from now")
	}
}
