package panel_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestUsersPageDeleteFormUsesDataConfirm verifies the per-user delete form uses
// a CSP-safe data-confirm attribute (consumed by panel.js) instead of an inline
// onclick handler, which would break under strict CSP and is inconsistent with
// the drawer/bulk delete confirms already wired via addEventListener.
func TestUsersPageDeleteFormUsesDataConfirm(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	seedAdmin(t, srv.DB)
	// Seed a user so the row delete form renders.
	repo := panel.UserRepo{DB: srv.DB}
	if _, err := repo.Create("alice", "00112233445566778899aabbccddeeff"); err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	cookie := doLogin(t, h, "admin", "correcthorsebatterystaple")

	req := httptest.NewRequest(http.MethodGet, "/p-example/users", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /users status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, bad := range []string{`onclick=`, `onsubmit=`} {
		if strings.Contains(body, bad) {
			t.Fatalf("users page must not render inline %s handler (breaks strict CSP):\n%s", bad, body)
		}
	}
	if !strings.Contains(body, `data-confirm="Delete alice?"`) {
		t.Fatalf("delete form must carry data-confirm=\"Delete alice?\":\n%s", body)
	}
}
