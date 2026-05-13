package panel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
)

func TestDashboardEventsRequireAuth(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/events", nil)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauthenticated /dashboard/events status = %d, want 303", rec.Code)
	}
}

func TestDashboardEventsRejectCrossSiteOrigin(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/events", nil)
	req.Host = "admin.example.test"
	req.Header.Set("Origin", "https://evil.example.test")
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site Origin status = %d, want 403", rec.Code)
	}
}

func TestDashboardEventsRejectOriginSchemeMismatch(t *testing.T) {
	s := newDashboardTestServer(t)
	s.Secure = true
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/events", nil)
	req.Host = "admin.example.test"
	req.Header.Set("Origin", "http://admin.example.test")
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("scheme-mismatched Origin status = %d, want 403", rec.Code)
	}
}

func TestDashboardEventsAllowSameOriginHTTPSWhenSecure(t *testing.T) {
	s := newDashboardTestServer(t)
	s.Secure = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/events", nil).WithContext(ctx)
	req.Host = "admin.example.test"
	req.Header.Set("Origin", "https://admin.example.test")
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("same-origin HTTPS status = %d, want 200", rec.Code)
	}
}

func TestDashboardEventsRejectCrossSiteFetchMetadata(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/events", nil)
	req.Host = "admin.example.test"
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site fetch metadata status = %d, want 403", rec.Code)
	}
}

func TestDashboardEventsHeaders(t *testing.T) {
	s := newDashboardTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/events", nil).WithContext(ctx)
	req.Host = "admin.example.test"
	req.Header.Set("Origin", "http://admin.example.test")
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /dashboard/events status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", got)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "'unsafe-inline'") {
		t.Fatalf("CSP = %q, must not allow unsafe-inline", csp)
	}
	for _, want := range []string{"default-src 'self'", "script-src 'self'", "style-src 'self'", "connect-src 'self'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP = %q, missing %q", csp, want)
		}
	}
}

func TestDashboardEventsLimitConcurrentStreams(t *testing.T) {
	limiter := newDashboardStreamLimiter(1)
	if !limiter.acquire("session-a") {
		t.Fatal("first stream should acquire")
	}
	if limiter.acquire("session-a") {
		t.Fatal("second stream for same session must be rejected")
	}
	limiter.release("session-a")
	if !limiter.acquire("session-a") {
		t.Fatal("stream should acquire after release")
	}
}

func TestDashboardStreamClosesWhenSessionDeleted(t *testing.T) {
	s := newDashboardTestServer(t)
	cookie := authCookieForTest(t, s)
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/events?period=24h", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	if _, err := s.DB.Exec(`DELETE FROM sessions WHERE id=?`, cookie.Value); err != nil {
		t.Fatal(err)
	}

	if keepOpen := s.writeDashboardEventBatch(rec, req, metrics.Period24h); keepOpen {
		t.Fatal("deleted session batch must end the stream")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: dashboard-close") {
		t.Fatalf("deleted session must emit dashboard-close, got:\n%s", body)
	}
}

func TestDashboardEventsDoNotExposeSensitiveTokens(t *testing.T) {
	s := newDashboardTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/events?period=24h", nil)
	req.Host = "admin.example.test"
	req.Header.Set("Origin", "http://admin.example.test")
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	if keepOpen := s.writeDashboardEventBatch(rec, req, metrics.Period24h); !keepOpen {
		t.Fatal("dashboard event batch closed stream unexpectedly")
	}

	assertNoSensitiveDashboardTokens(t, rec.Body.String())
}

func TestDashboardEventsCanceledContextReturnsPromptly(t *testing.T) {
	s := newDashboardTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/events", nil).WithContext(ctx)
	req.Host = "admin.example.test"
	req.Header.Set("Origin", "http://admin.example.test")
	req.AddCookie(authCookieForTest(t, s))
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Handler().ServeHTTP(rec, req)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dashboard SSE handler did not return after context cancellation")
	}
}

func TestSessionTouchSkippedForDashboardEvents(t *testing.T) {
	s := newDashboardTestServer(t)
	cookie := authCookieForTest(t, s)
	original := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if _, err := s.DB.Exec(`UPDATE sessions SET last_seen_at=? WHERE id=?`, original, cookie.Value); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/events", nil).WithContext(ctx)
	req.Host = "admin.example.test"
	req.Header.Set("Origin", "http://admin.example.test")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	got := sessionLastSeenForTest(t, s, cookie.Value)
	if got != original {
		t.Fatalf("dashboard/events touched last_seen_at = %q, want unchanged %q", got, original)
	}
}

func TestSessionTouchSkippedForDashboardFragments(t *testing.T) {
	s := newDashboardTestServer(t)
	cookie := authCookieForTest(t, s)
	original := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if _, err := s.DB.Exec(`UPDATE sessions SET last_seen_at=? WHERE id=?`, original, cookie.Value); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard/fragments/health", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard fragment status = %d, want 200", rec.Code)
	}
	got := sessionLastSeenForTest(t, s, cookie.Value)
	if got != original {
		t.Fatalf("dashboard fragment touched last_seen_at = %q, want unchanged %q", got, original)
	}
}

func TestSessionTouchEnabledForDashboardPage(t *testing.T) {
	s := newDashboardTestServer(t)
	cookie := authCookieForTest(t, s)
	original := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if _, err := s.DB.Exec(`UPDATE sessions SET last_seen_at=? WHERE id=?`, original, cookie.Value); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/p-example/dashboard", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", rec.Code)
	}
	got := sessionLastSeenForTest(t, s, cookie.Value)
	if got == original {
		t.Fatalf("dashboard page did not update last_seen_at; still %q", got)
	}
}

func sessionLastSeenForTest(t *testing.T, s *Server, sessionID string) string {
	t.Helper()
	var lastSeen string
	if err := s.DB.QueryRow(`SELECT last_seen_at FROM sessions WHERE id=?`, sessionID).Scan(&lastSeen); err != nil {
		t.Fatal(err)
	}
	return lastSeen
}
