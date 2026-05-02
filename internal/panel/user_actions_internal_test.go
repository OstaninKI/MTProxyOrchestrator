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
	old := WriteAndReloadHook
	WriteAndReloadHook = func(_ string, data []byte) error {
		hook(data)
		return nil
	}
	t.Cleanup(func() { WriteAndReloadHook = old })
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
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("create user response must disable caching, got %q", cacheControl)
	}
}

func withWriteAndReloadError(t *testing.T, err error) {
	t.Helper()
	old := WriteAndReloadHook
	WriteAndReloadHook = func(_ string, _ []byte) error {
		return err
	}
	t.Cleanup(func() { WriteAndReloadHook = old })
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

// withBridgeActive injects a stub for isSingboxActive so tests can control
// whether Bridge mode is considered active without touching the real system.
func withBridgeActive(t *testing.T, active bool) {
	t.Helper()
	old := isSingboxActive
	isSingboxActive = func() bool { return active }
	t.Cleanup(func() { isSingboxActive = old })
}

// TestReloadTeleproxyBridgeModePreservesSocks5 verifies that reloadTeleproxy renders
// the SOCKS5 upstream when Bridge mode is active.
func TestReloadTeleproxyBridgeModePreservesSocks5(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	if _, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}

	withBridgeActive(t, true)
	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

	if err := srv.reloadTeleproxy(); err != nil {
		t.Fatalf("reloadTeleproxy error: %v", err)
	}

	got := string(applied)
	const wantSOCKS5 = `socks5 = "127.0.0.1:1080"`
	if !strings.Contains(got, wantSOCKS5) {
		t.Fatalf("Bridge mode: applied config missing SOCKS5 upstream.\ngot:\n%s", got)
	}
}

// TestReloadTeleproxySingleModeNoSocks5 verifies that reloadTeleproxy renders
// no SOCKS5 upstream when Single mode is active.
func TestReloadTeleproxySingleModeNoSocks5(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	if _, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}

	withBridgeActive(t, false)
	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

	if err := srv.reloadTeleproxy(); err != nil {
		t.Fatalf("reloadTeleproxy error: %v", err)
	}

	got := string(applied)
	if strings.Contains(got, "socks5") {
		t.Fatalf("Single mode: applied config must not contain socks5.\ngot:\n%s", got)
	}
}

// TestUserCreateBridgeModePreservesSocks5 verifies that creating a user preserves
// the SOCKS5 upstream in the rendered Teleproxy config when Bridge mode is active.
func TestUserCreateBridgeModePreservesSocks5(t *testing.T) {
	srv := newInternalTestServer(t)
	withBridgeActive(t, true)
	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

	form := url.Values{
		CSRFField(): {"token"},
		"label":     {"bob"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/users/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "token"})
	rec := httptest.NewRecorder()

	srv.handleUserCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("create user status = %d, want 200", rec.Code)
	}
	got := string(applied)
	const wantSOCKS5 = `socks5 = "127.0.0.1:1080"`
	if !strings.Contains(got, wantSOCKS5) {
		t.Fatalf("Bridge mode: create user did not preserve SOCKS5 upstream.\ngot:\n%s", got)
	}
}

// TestUserCreateSingleModeNoSocks5 verifies that creating a user in Single mode
// does not render any SOCKS5 upstream.
func TestUserCreateSingleModeNoSocks5(t *testing.T) {
	srv := newInternalTestServer(t)
	withBridgeActive(t, false)
	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

	form := url.Values{
		CSRFField(): {"token"},
		"label":     {"charlie"},
	}
	req := httptest.NewRequest(http.MethodPost, "/p-example/users/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "token"})
	rec := httptest.NewRecorder()

	srv.handleUserCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("create user status = %d, want 200", rec.Code)
	}
	got := string(applied)
	if strings.Contains(got, "socks5") {
		t.Fatalf("Single mode: create user must not render SOCKS5 upstream.\ngot:\n%s", got)
	}
}

// TestUserToggleBridgeModePreservesSocks5 verifies that toggling a user preserves
// the SOCKS5 upstream when Bridge mode is active.
func TestUserToggleBridgeModePreservesSocks5(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	withBridgeActive(t, true)
	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

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

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("toggle status = %d, want 303", rec.Code)
	}
	got := string(applied)
	const wantSOCKS5 = `socks5 = "127.0.0.1:1080"`
	if !strings.Contains(got, wantSOCKS5) {
		t.Fatalf("Bridge mode: toggle did not preserve SOCKS5 upstream.\ngot:\n%s", got)
	}
}

// TestUserToggleSingleModeNoSocks5 verifies that toggling a user in Single mode
// does not render any SOCKS5 upstream.
func TestUserToggleSingleModeNoSocks5(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	withBridgeActive(t, false)
	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

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

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("toggle status = %d, want 303", rec.Code)
	}
	got := string(applied)
	if strings.Contains(got, "socks5") {
		t.Fatalf("Single mode: toggle must not render SOCKS5 upstream.\ngot:\n%s", got)
	}
}

// TestUserDeleteBridgeModePreservesSocks5 verifies that deleting a user preserves
// the SOCKS5 upstream when Bridge mode is active.
func TestUserDeleteBridgeModePreservesSocks5(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create("bob", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatal(err)
	}
	withBridgeActive(t, true)
	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

	req := httptest.NewRequest(http.MethodPost, "/users/1/delete", strings.NewReader(url.Values{
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

	srv.handleUserDelete(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want 303", rec.Code)
	}
	got := string(applied)
	const wantSOCKS5 = `socks5 = "127.0.0.1:1080"`
	if !strings.Contains(got, wantSOCKS5) {
		t.Fatalf("Bridge mode: delete did not preserve SOCKS5 upstream.\ngot:\n%s", got)
	}
}

// TestUserDeleteSingleModeNoSocks5 verifies that deleting a user in Single mode
// does not render any SOCKS5 upstream.
func TestUserDeleteSingleModeNoSocks5(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create("bob", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatal(err)
	}
	withBridgeActive(t, false)
	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

	req := httptest.NewRequest(http.MethodPost, "/users/1/delete", strings.NewReader(url.Values{
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

	srv.handleUserDelete(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want 303", rec.Code)
	}
	got := string(applied)
	if strings.Contains(got, "socks5") {
		t.Fatalf("Single mode: delete must not render SOCKS5 upstream.\ngot:\n%s", got)
	}
}

// TestUserRotateBridgeModePreservesSocks5 verifies that rotating a user's secret
// preserves the SOCKS5 upstream when Bridge mode is active.
func TestUserRotateBridgeModePreservesSocks5(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	withBridgeActive(t, true)
	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

	req := httptest.NewRequest(http.MethodPost, "/users/1/rotate", strings.NewReader(url.Values{
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

	srv.handleUserRotate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, want 200", rec.Code)
	}
	got := string(applied)
	const wantSOCKS5 = `socks5 = "127.0.0.1:1080"`
	if !strings.Contains(got, wantSOCKS5) {
		t.Fatalf("Bridge mode: rotate did not preserve SOCKS5 upstream.\ngot:\n%s", got)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("rotate user response must disable caching, got %q", cacheControl)
	}
}

// TestUserRotateSingleModeNoSocks5 verifies that rotating a user's secret in Single mode
// does not render any SOCKS5 upstream.
func TestUserRotateSingleModeNoSocks5(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	withBridgeActive(t, false)
	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

	req := httptest.NewRequest(http.MethodPost, "/users/1/rotate", strings.NewReader(url.Values{
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

	srv.handleUserRotate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, want 200", rec.Code)
	}
	got := string(applied)
	if strings.Contains(got, "socks5") {
		t.Fatalf("Single mode: rotate must not render SOCKS5 upstream.\ngot:\n%s", got)
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
