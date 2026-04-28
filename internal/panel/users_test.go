package panel_test

import (
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
)

func newRepo(t *testing.T) panel.UserRepo {
	t.Helper()
	return panel.UserRepo{DB: newTestDB(t)}
}

func TestUserRepoCreate(t *testing.T) {
	r := newRepo(t)
	id, err := r.Create("alice", "aabbccddeeff00112233445566778899")
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestUserRepoDuplicateLabel(t *testing.T) {
	r := newRepo(t)
	if _, err := r.Create("alice", "aabbccddeeff00112233445566778899"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create("alice", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err == nil {
		t.Error("expected error for duplicate label")
	}
}

func TestUserRepoLimit(t *testing.T) {
	r := newRepo(t)
	for i := range 16 {
		label := "user" + string(rune('a'+i))
		sec := "aabbccddeeff001122334455667788" + string(rune('0'+i)) + "0"
		if _, err := r.Create(label, "aabbccddeeff00112233445566778800"); err != nil {
			_ = label
			_ = sec
			t.Fatalf("failed at user %d: %v", i, err)
		}
	}
	if _, err := r.Create("extra", "aabbccddeeff00112233445566778899"); err == nil {
		t.Error("expected error when exceeding 16 users")
	}
}

func TestUserRepoEnableDisable(t *testing.T) {
	r := newRepo(t)
	id, _ := r.Create("bob", "aabbccddeeff00112233445566778899")
	if err := r.SetEnabled(id, false); err != nil {
		t.Fatal(err)
	}
	users, _ := r.List()
	for _, u := range users {
		if u.ID == id && u.Enabled {
			t.Error("user should be disabled")
		}
	}
	if err := r.SetEnabled(id, true); err != nil {
		t.Fatal(err)
	}
}

func TestUserRepoSoftDelete(t *testing.T) {
	r := newRepo(t)
	id, _ := r.Create("carol", "aabbccddeeff00112233445566778899")
	if err := r.Delete(id); err != nil {
		t.Fatal(err)
	}
	users, _ := r.List()
	for _, u := range users {
		if u.ID == id {
			t.Error("deleted user should not appear in list")
		}
	}
}
