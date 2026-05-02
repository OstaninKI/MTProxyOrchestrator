package panel_test

import (
	"net/http"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func doLogin(t *testing.T, h http.Handler, login, password string) *http.Cookie {
	t.Helper()
	w := postLoginForm(h, login, password)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("doLogin: want 303, got %d body: %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			return c
		}
	}
	t.Fatal("doLogin: no session_id cookie in response")
	return nil
}
