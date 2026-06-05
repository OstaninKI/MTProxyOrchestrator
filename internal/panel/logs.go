package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/logstream"
)

var wsUpgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	// CheckOrigin validates that the request origin matches the request Host.
	// This prevents cross-site WebSocket hijacking.
	CheckOrigin: func(r *http.Request) bool {
		return wsOriginAllowed(r.Header.Get("Origin"), r.Host)
	},
}

// wsOriginAllowed reports whether a browser WebSocket Origin is allowed for a
// request with the given Host. An empty origin (non-browser clients such as
// curl) is allowed.
//
// The comparison ignores the port: behind nginx the forwarded Host carries no
// port (proxy_set_header Host $host), while the browser's Origin includes the
// public port whenever the panel is served on a non-standard port (e.g. :8443).
// Comparing only the hostname still rejects a different origin domain, which is
// what matters for cross-site hijacking.
func wsOriginAllowed(origin, host string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(stripPort(u.Host), stripPort(host))
}

// stripPort returns hostport without its trailing :port, if present.
func stripPort(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

// handleLogsPage serves the logs HTML page (requires auth via middleware).
func (s *Server) handleLogsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	panelPath := strings.TrimSuffix(s.PanelPath, "/")
	csrfToken, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "failed to create csrf token", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, csrfToken, s.Secure, s.PanelPath)
	logsPage(w, logsPageData{
		PanelPath: normalizePanelPath(s.PanelPath),
		BasePath:  panelPath,
		CSRFToken: csrfToken,
	})
}

// handleLogsStream upgrades to a WebSocket and streams log entries.
// Authentication is enforced by the requireAuth middleware before this handler.
func (s *Server) handleLogsStream(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	component := q.Get("component")
	if component == "" {
		component = logstream.ComponentPanel
	}
	levelStr := q.Get("level")
	if levelStr == "" {
		levelStr = "info"
	}
	search := q.Get("q")

	filter := logstream.Filter{
		MinLevel: logstream.ParseLevel(levelStr),
		Search:   search,
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade writes an error response itself on failure.
		return
	}
	defer conn.Close()

	ctx := r.Context()
	ch := make(chan logstream.LogEntry, 64)

	// Start streaming in a goroutine. The error is captured so the loop below
	// can report it to the browser; close(ch) happens-after the assignment, so
	// reading streamErr after the channel closes is safe.
	var streamErr error
	go func() {
		streamErr = logstream.Stream(ctx, component, filter, ch)
		close(ch)
	}()

	// Forward entries to the WebSocket client.
	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				// Stream ended. If it failed for a reason other than the client
				// disconnecting, tell the browser why with a close frame rather
				// than dropping the TCP connection — a bare drop surfaces as an
				// opaque 1006 and an endless reconnect loop in the UI.
				closeLogStream(conn, streamErr)
				return
			}
			data, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// closeLogStream sends a WebSocket close frame describing why the log stream
// ended. A nil error or a context cancellation (client navigated away, server
// shutting down) is reported as a normal closure; any other error is reported
// as an internal error with a human-readable reason. The reason carries no
// secrets — logstream errors are journalctl process/exec failures.
func closeLogStream(conn *websocket.Conn, err error) {
	code := websocket.CloseNormalClosure
	reason := ""
	if err != nil && !errors.Is(err, context.Canceled) {
		code = websocket.CloseInternalServerErr
		reason = "log stream unavailable: " + err.Error()
	}
	// Close reason payloads are capped at 123 bytes by the protocol.
	if len(reason) > 123 {
		reason = reason[:123]
	}
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		time.Now().Add(5*time.Second),
	)
}

// handleLogsDownload returns the last N log lines as plain text.
// Authentication is enforced by the requireAuth middleware before this handler.
func (s *Server) handleLogsDownload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	component := q.Get("component")
	if component == "" {
		component = logstream.ComponentPanel
	}

	ctx := r.Context()
	entries, err := logstream.Download(ctx, component, 500)
	if err != nil {
		http.Error(w, "failed to read logs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.log"`, component))
	for _, e := range entries {
		fmt.Fprintf(w, "%s [%s] %s\n", e.Time.Format(time.RFC3339), e.Level, e.Message)
	}
}
