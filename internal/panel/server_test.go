package panel_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
)

func newTestServer(t *testing.T, panelPath string) *panel.Server {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &panel.Server{
		DB:          d,
		PanelPath:   panelPath,
		RateLimiter: panel.NewRateLimiter(),
		Secure:      false,
	}
}

func TestOutsidePanelPathReturns404(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	for _, path := range []string{"/", "/login", "/admin", "/p-other/"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s: want 404, got %d", path, w.Code)
		}
	}
}

func TestLoginPageServedUnderPanelPath(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/p-example/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("GET /p-example/login: want 200, got %d", w.Code)
	}
}

func TestDashboardRedirectsUnauthenticated(t *testing.T) {
	srv := newTestServer(t, "/p-example/")
	h := srv.Handler()

	r := httptest.NewRequest(http.MethodGet, "/p-example/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Errorf("unauthenticated dashboard: want 303, got %d", w.Code)
	}
}
