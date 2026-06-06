package panel

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
)

const (
	dashboardSSEMaxStreamsPerSession = 1
	dashboardSSEMaxLifetime          = 10 * time.Minute
	dashboardSSETickInterval         = 5 * time.Second
)

type dashboardStreamLimiter struct {
	mu       sync.Mutex
	max      int
	sessions map[string]int
}

func newDashboardStreamLimiter(max int) *dashboardStreamLimiter {
	if max < 1 {
		max = 1
	}
	return &dashboardStreamLimiter{
		max:      max,
		sessions: make(map[string]int),
	}
}

func (l *dashboardStreamLimiter) acquire(sessionID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sessions[sessionID] >= l.max {
		return false
	}
	l.sessions[sessionID]++
	return true
}

func (l *dashboardStreamLimiter) release(sessionID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sessions[sessionID] <= 1 {
		delete(l.sessions, sessionID)
		return
	}
	l.sessions[sessionID]--
}

var defaultDashboardStreamLimiter = newDashboardStreamLimiter(dashboardSSEMaxStreamsPerSession)

func (s *Server) dashboardRequestAllowed(r *http.Request) bool {
	site := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))
	if site == "cross-site" {
		return false
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return site == "" || site == "same-origin" || site == "none"
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return parsed.Scheme == dashboardRequestScheme(r, s.Secure) && parsed.Host == r.Host
}

func dashboardRequestScheme(r *http.Request, secure bool) string {
	if secure || r.TLS != nil {
		return "https"
	}
	return "http"
}

func (s *Server) handleDashboardEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.ContentLength > 0 {
		http.Error(w, "request body not allowed", http.StatusBadRequest)
		return
	}
	if !s.dashboardRequestAllowed(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !defaultDashboardStreamLimiter.acquire(cookie.Value) {
		http.Error(w, "too many dashboard streams", http.StatusTooManyRequests)
		return
	}
	defer defaultDashboardStreamLimiter.release(cookie.Value)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	setStrictPanelCSP(w)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	period := metrics.ParsePeriod(r.URL.Query().Get("period"))
	if !s.writeDashboardEventBatch(w, r, period) {
		flusher.Flush()
		return
	}
	flusher.Flush()

	lifetime := time.NewTimer(dashboardSSEMaxLifetime)
	defer lifetime.Stop()

	ticker := time.NewTicker(dashboardSSETickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-lifetime.C:
			writeSSE(w, "dashboard-close", "max-lifetime")
			flusher.Flush()
			return
		case <-ticker.C:
			if !s.writeDashboardEventBatch(w, r, period) {
				flusher.Flush()
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) writeDashboardEventBatch(w http.ResponseWriter, r *http.Request, period metrics.Period) bool {
	if !s.isAuthenticated(r) {
		writeSSE(w, "dashboard-close", "session-expired")
		return false
	}

	writeSSE(w, "dashboard-health", "refresh")
	writeSSE(w, "dashboard-connections", "refresh")
	writeSSE(w, "dashboard-traffic", string(period))
	writeSSE(w, "dashboard-components", "refresh")
	writeSSE(w, "dashboard-ops", "refresh")
	writeSSE(w, "dashboard-upstream", "refresh")
	writeSSE(w, "dashboard-heartbeat", time.Now().UTC().Format(time.RFC3339))
	return true
}

func writeSSE(w http.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(data, "\n", " "))
}
