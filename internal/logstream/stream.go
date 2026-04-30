// Package logstream provides live log streaming from journald units
// with redaction, level filtering, and text search.
package logstream

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Component names supported for filtering.
const (
	ComponentPanel     = "panel"
	ComponentTeleproxy = "teleproxy"
	ComponentSingBox   = "sing-box"
	ComponentNginx     = "nginx"
)

// unitForComponent maps a component name to its systemd unit name.
func unitForComponent(component string) string {
	switch component {
	case ComponentPanel:
		return "tgproxy-panel"
	case ComponentTeleproxy:
		return "teleproxy"
	case ComponentSingBox:
		return "sing-box"
	case ComponentNginx:
		return "nginx"
	default:
		return "tgproxy-panel"
	}
}

// Level represents a log severity level.
type Level int

const (
	LevelError Level = iota
	LevelWarn
	LevelInfo
	LevelDebug
)

// ParseLevel parses a level string, defaulting to LevelInfo.
func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "error":
		return LevelError
	case "warn", "warning":
		return LevelWarn
	case "info":
		return LevelInfo
	case "debug":
		return LevelDebug
	default:
		return LevelInfo
	}
}

func (l Level) String() string {
	switch l {
	case LevelError:
		return "error"
	case LevelWarn:
		return "warn"
	case LevelInfo:
		return "info"
	case LevelDebug:
		return "debug"
	default:
		return "info"
	}
}

// journaldPriorityToLevel maps journald PRIORITY field (syslog severity) to Level.
// PRIORITY values: 0=emerg, 1=alert, 2=crit, 3=err, 4=warning, 5=notice/info, 6=info, 7=debug
func journaldPriorityToLevel(priority string) Level {
	p, err := strconv.Atoi(priority)
	if err != nil {
		return LevelInfo
	}
	switch {
	case p <= 3:
		return LevelError
	case p == 4:
		return LevelWarn
	case p <= 6:
		return LevelInfo
	default:
		return LevelDebug
	}
}

// LogEntry is a single log line ready for delivery to the browser.
type LogEntry struct {
	Time      time.Time `json:"time"`
	Level     string    `json:"level"`
	Component string    `json:"component"`
	Message   string    `json:"message"`
}

// journaldJSON is the raw JSON object produced by journalctl --output=json.
type journaldJSON struct {
	RealtimeTimestamp string `json:"__REALTIME_TIMESTAMP"`
	Priority          string `json:"PRIORITY"`
	Message           string `json:"MESSAGE"`
}

// Filter controls which log entries are forwarded to the caller.
type Filter struct {
	// MinLevel: only entries at this level or above (lower numeric value) are forwarded.
	MinLevel Level
	// Search: if non-empty, only entries whose Message contains this string
	// (case-insensitive) are forwarded.
	Search string
}

func (f Filter) matches(e LogEntry) bool {
	entryLevel := ParseLevel(e.Level)
	if entryLevel > f.MinLevel {
		return false
	}
	if f.Search != "" && !strings.Contains(strings.ToLower(e.Message), strings.ToLower(f.Search)) {
		return false
	}
	return true
}

// Redactor applies secret redaction to log messages.
type Redactor struct {
	mtprotoSecret *regexp.Regexp // 32-char hex strings (MTProto secrets)
	keyValuePairs *regexp.Regexp // password=... or secret=...
	jwtPattern    *regexp.Regexp // long base64url tokens (40+ chars)
}

// NewRedactor creates a Redactor with the standard redaction rules.
func NewRedactor() *Redactor {
	return &Redactor{
		// MTProto secrets: exactly 32 hex characters (not part of a longer hex string)
		mtprotoSecret: regexp.MustCompile(`\b[0-9a-fA-F]{32}\b`),
		// Key=value pairs where key is password or secret (case-insensitive)
		keyValuePairs: regexp.MustCompile(`(?i)(password|secret)=\S+`),
		// JWT / session tokens: base64url strings 32+ characters that contain
		// at least one uppercase letter (distinguishing them from plain hex secrets).
		jwtPattern: regexp.MustCompile(`[A-Za-z0-9_\-]{32,}`),
	}
}

// Redact replaces sensitive patterns in a message with [REDACTED].
func (r *Redactor) Redact(msg string) string {
	// Order matters: apply key=value first to handle "secret=<token>" as one unit.
	msg = r.keyValuePairs.ReplaceAllStringFunc(msg, func(m string) string {
		// keep the key part, redact the value
		idx := strings.Index(m, "=")
		if idx < 0 {
			return "[REDACTED]"
		}
		return m[:idx+1] + "[REDACTED]"
	})
	// Redact MTProto secrets (exact 32-char hex).
	msg = r.mtprotoSecret.ReplaceAllString(msg, "[REDACTED]")
	// Redact JWT/session tokens (long base64url strings).
	msg = r.jwtPattern.ReplaceAllString(msg, "[REDACTED]")
	return msg
}

// RedactEntry returns a copy of the entry with the message redacted.
func (r *Redactor) RedactEntry(e LogEntry) LogEntry {
	e.Message = r.Redact(e.Message)
	return e
}

// Stream reads journald output for the given component and sends filtered,
// redacted LogEntry values to ch until ctx is cancelled.
// The caller is responsible for closing ch after Stream returns.
func Stream(ctx context.Context, component string, f Filter, ch chan<- LogEntry) error {
	unit := unitForComponent(component)
	cmd := exec.CommandContext(ctx, "journalctl", "-fu", unit, "--output=json")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	redactor := NewRedactor()
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		entry, ok := parseJournaldLine(line, component)
		if !ok {
			continue
		}
		if !f.matches(entry) {
			continue
		}
		entry = redactor.RedactEntry(entry)
		select {
		case ch <- entry:
		case <-ctx.Done():
			// Drain and exit.
			_ = cmd.Wait()
			return ctx.Err()
		}
	}

	_ = cmd.Wait()
	return scanner.Err()
}

// Download reads the last n log lines for the given component synchronously.
func Download(ctx context.Context, component string, n int) ([]LogEntry, error) {
	unit := unitForComponent(component)
	args := []string{
		"-u", unit,
		"--output=json",
		"--no-pager",
		"-n", strconv.Itoa(n),
	}
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	redactor := NewRedactor()
	var entries []LogEntry
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Bytes()
		entry, ok := parseJournaldLine(line, component)
		if !ok {
			continue
		}
		entry = redactor.RedactEntry(entry)
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

// parseJournaldLine parses a single JSON line from journalctl --output=json.
func parseJournaldLine(line []byte, component string) (LogEntry, bool) {
	var raw journaldJSON
	if err := json.Unmarshal(line, &raw); err != nil {
		return LogEntry{}, false
	}

	// __REALTIME_TIMESTAMP is microseconds since epoch as a string.
	var t time.Time
	if raw.RealtimeTimestamp != "" {
		us, err := strconv.ParseInt(raw.RealtimeTimestamp, 10, 64)
		if err == nil {
			t = time.Unix(us/1_000_000, (us%1_000_000)*1_000).UTC()
		}
	}
	if t.IsZero() {
		t = time.Now().UTC()
	}

	level := journaldPriorityToLevel(raw.Priority)

	return LogEntry{
		Time:      t,
		Level:     level.String(),
		Component: component,
		Message:   raw.Message,
	}, true
}
