package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/logstream"
)

var wsUpgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	// CheckOrigin validates that the request origin matches the Host header.
	// This prevents cross-site WebSocket hijacking.
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Non-browser clients (curl, etc.) send no Origin; allow them.
			return true
		}
		// Strip scheme from origin and compare to Host.
		origin = strings.TrimPrefix(origin, "https://")
		origin = strings.TrimPrefix(origin, "http://")
		return origin == r.Host
	},
}

// handleLogsPage serves the logs HTML page (requires auth via middleware).
func (s *Server) handleLogsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	panelPath := strings.TrimSuffix(s.PanelPath, "/")
	logsPage(w, panelPath)
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

	// Start streaming in a goroutine; errors are ignored (client disconnect).
	go func() {
		_ = logstream.Stream(ctx, component, filter, ch)
		close(ch)
	}()

	// Forward entries to the WebSocket client.
	for {
		select {
		case entry, ok := <-ch:
			if !ok {
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
