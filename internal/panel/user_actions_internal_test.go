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

func TestReloadTeleproxyExcludesSuspendedUsers(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	if _, err := repo.Create("active", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	suspendedID, err := repo.Create("suspended", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSuspended(suspendedID, true); err != nil {
		t.Fatal(err)
	}

	var applied []byte
	withWriteAndReloadHook(t, func(data []byte) {
		applied = append([]byte(nil), data...)
	})

	if err := srv.reloadTeleproxy(); err != nil {
		t.Fatalf("reloadTeleproxy error: %v", err)
	}
	got := string(applied)
	if !strings.Contains(got, `label = "active"`) {
		t.Fatalf("active user missing from applied config: %s", got)
	}
	if strings.Contains(got, "suspended") {
		t.Fatalf("suspended user must not appear: %s", got)
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

func TestUserDeleteAuditsRollback(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	withWriteAndReloadError(t, errApplyFailed)

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

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("delete status = %d, want 500", rec.Code)
	}

	// Check that user.delete_rollback audit entry exists
	var count int
	if err := srv.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='user.delete_rollback' AND target='alice'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 audit rollback entry, got %d", count)
	}
}

func TestUserRotateAuditsRollback(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	withWriteAndReloadError(t, errApplyFailed)

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

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("rotate status = %d, want 500", rec.Code)
	}

	// Check that user_rotate_rollback audit entry exists
	var count int
	if err := srv.DB.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='user.rotate_rollback' AND target='alice'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 audit rollback entry, got %d", count)
	}
}

func quotaPostReq(id int64, form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/users/x/quota", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "token"})
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, &chi.Context{
		URLParams: chi.RouteParams{
			Keys:   []string{"id"},
			Values: []string{strconv.FormatInt(id, 10)},
		},
	}))
}

func TestUserQuotaSetRestoresDBWhenTeleproxyApplyFails(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetQuota(id, 1024*1024*1024, "daily", 70, 1000); err != nil {
		t.Fatal(err)
	}
	users, _ := repo.List()
	prev := users[0]

	withWriteAndReloadError(t, errApplyFailed)

	form := url.Values{
		CSRFField(): {"token"},
		"gb":        {"5"},
		"period":    {"weekly"},
		"warn_pct":  {"90"},
	}
	rec := httptest.NewRecorder()
	srv.handleUserQuotaSet(rec, quotaPostReq(id, form))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	users, err = repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("unexpected user list: %+v", users)
	}
	got := users[0]
	if got.QuotaBytes != prev.QuotaBytes || got.QuotaPeriod != prev.QuotaPeriod ||
		got.QuotaWarnPct != prev.QuotaWarnPct || got.QuotaPeriodStart != prev.QuotaPeriodStart {
		t.Fatalf("quota fields not restored: got=%+v want=%+v", got, prev)
	}
}

func TestUserQuotaResetRestoresDBWhenTeleproxyApplyFails(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetQuota(id, 1024*1024*1024, "daily", 70, 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB.Exec(
		`UPDATE users SET quota_used_bytes=?, quota_warned=1, quota_suspended=1 WHERE id=?`,
		int64(500*1024*1024), id,
	); err != nil {
		t.Fatal(err)
	}
	users, _ := repo.List()
	prev := users[0]

	withWriteAndReloadError(t, errApplyFailed)

	form := url.Values{CSRFField(): {"token"}}
	rec := httptest.NewRecorder()
	srv.handleUserQuotaReset(rec, quotaPostReq(id, form))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	users, err = repo.List()
	if err != nil {
		t.Fatal(err)
	}
	got := users[0]
	if got.QuotaUsedBytes != prev.QuotaUsedBytes || got.QuotaWarned != prev.QuotaWarned ||
		got.QuotaSuspended != prev.QuotaSuspended || got.QuotaPeriodStart != prev.QuotaPeriodStart {
		t.Fatalf("quota counters not restored: got=%+v want=%+v", got, prev)
	}
}

func TestUserSuspendToggleRestoresDBWhenTeleproxyApplyFails(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	withWriteAndReloadError(t, errApplyFailed)

	form := url.Values{CSRFField(): {"token"}}
	rec := httptest.NewRecorder()
	srv.handleUserSuspendToggle(rec, quotaPostReq(id, form))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	users, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if users[0].QuotaSuspended {
		t.Fatalf("suspended state not restored: %+v", users[0])
	}
}

func TestUserQuotaActionsAuditTargetIsLabel(t *testing.T) {
	srv := newInternalTestServer(t)
	repo := UserRepo{DB: srv.DB}
	id, err := repo.Create("alice", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	withWriteAndReloadHook(t, func(_ []byte) {})

	srv.handleUserQuotaSet(httptest.NewRecorder(), quotaPostReq(id, url.Values{
		CSRFField(): {"token"},
		"gb":        {"1"},
		"period":    {"daily"},
		"warn_pct":  {"80"},
	}))
	srv.handleUserQuotaReset(httptest.NewRecorder(), quotaPostReq(id, url.Values{
		CSRFField(): {"token"},
	}))
	srv.handleUserSuspendToggle(httptest.NewRecorder(), quotaPostReq(id, url.Values{
		CSRFField(): {"token"},
	}))

	for _, action := range []string{"user.quota_set", "user.quota_reset", "user.suspend"} {
		var target string
		if err := srv.DB.QueryRow(
			`SELECT target FROM audit_log WHERE action=? ORDER BY id DESC LIMIT 1`, action,
		).Scan(&target); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if target != "alice" {
			t.Fatalf("%s: audit target = %q, want %q", action, target, "alice")
		}
		if target == strconv.FormatInt(id, 10) {
			t.Fatalf("%s: audit target is id, expected label", action)
		}
	}
}
