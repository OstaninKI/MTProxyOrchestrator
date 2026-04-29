package panel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

var errApplyFailed = errors.New("apply failed")

func newInternalTestServer(t *testing.T) *Server {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &Server{
		DB:          d,
		PanelPath:   "/p-example/",
		RateLimiter: NewRateLimiter(),
		Secure:      false,
	}
}

func withWriteAndReloadHook(t *testing.T, hook func([]byte)) {
	t.Helper()
	old := writeAndReload
	writeAndReload = func(_ string, data []byte) error {
		hook(data)
		return nil
	}
	t.Cleanup(func() { writeAndReload = old })
}

func TestUserCreateAppliesTeleproxyConfig(t *testing.T) {
	srv := newInternalTestServer(t)
	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

	form := url.Values{
		CSRFField(): {"token"},
		"label":     {"alice"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/users/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "token"})
	rec := httptest.NewRecorder()

	srv.handleUserCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("create user status = %d, want 200", rec.Code)
	}
	if len(applied) == 0 {
		t.Fatal("creating a user must apply rendered teleproxy config")
	}
	if !strings.Contains(string(applied), `label = "alice"`) {
		t.Fatalf("applied config does not include created user: %s", applied)
	}
}

func withWriteAndReloadError(t *testing.T, err error) {
	t.Helper()
	old := writeAndReload
	writeAndReload = func(_ string, _ []byte) error {
		return err
	}
	t.Cleanup(func() { writeAndReload = old })
}

func TestReloadTeleproxyAppliesOnlyEnabledNonDeletedUsers(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	activeID, err := repo.Create("active", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	disabledID, err := repo.Create("disabled", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	deletedID, err := repo.Create("deleted", "cccccccccccccccccccccccccccccccc")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetEnabled(disabledID, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(deletedID); err != nil {
		t.Fatal(err)
	}

	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

	srv.reloadTeleproxy()

	if len(applied) == 0 {
		t.Fatal("reloadTeleproxy must apply rendered config")
	}
	got := string(applied)
	if !strings.Contains(got, `label = "active"`) {
		t.Fatalf("applied config missing active user %d: %s", activeID, got)
	}
	if strings.Contains(got, "disabled") || strings.Contains(got, "deleted") {
		t.Fatalf("applied config includes inactive users: %s", got)
	}
}

func TestUserToggleRestoresDBWhenTeleproxyApplyFails(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	withWriteAndReloadError(t, errApplyFailed)

	req := httptest.NewRequest(http.MethodPost, "/users/1/toggle", strings.NewReader(url.Values{
		CSRFField(): {"token"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "token"})
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, &chi.Context{
		URLParams: chi.RouteParams{
			Keys:   []string{"id"},
			Values: []string{strconv.FormatInt(id, 10)},
		},
	}))
	rec := httptest.NewRecorder()

	srv.handleUserToggle(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	users, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != id || !users[0].Enabled {
		t.Fatalf("user state was not restored after apply failure: %+v", users)
	}
}
