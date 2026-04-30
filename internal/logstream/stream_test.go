package logstream

import (
	"strings"
	"testing"
)

// ---- Redactor tests ----

func TestRedactor_MTProtoSecret(t *testing.T) {
	r := NewRedactor()
	secret := "deadbeefdeadbeefdeadbeefdeadbeef" // exactly 32 hex chars
	msg := "user connected with secret " + secret + " from 1.2.3.4"
	got := r.Redact(msg)
	if strings.Contains(got, secret) {
		t.Errorf("expected secret to be redacted, got: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in output, got: %s", got)
	}
}

func TestRedactor_ShortHexNotRedacted(t *testing.T) {
	r := NewRedactor()
	// 31 hex chars — must NOT be redacted to avoid false positives.
	short := "deadbeefdeadbeefdeadbeefdeadbee" // 31 chars
	msg := "token: " + short + " end"
	got := r.Redact(msg)
	if strings.Contains(got, "[REDACTED]") {
		t.Errorf("short hex string should NOT be redacted, got: %s", got)
	}
}

func TestRedactor_PasswordKeyValue(t *testing.T) {
	r := NewRedactor()
	msg := "auth attempt password=hunter2 from 10.0.0.1"
	got := r.Redact(msg)
	if strings.Contains(got, "hunter2") {
		t.Errorf("password value should be redacted, got: %s", got)
	}
	if !strings.Contains(got, "password=[REDACTED]") {
		t.Errorf("expected password=[REDACTED], got: %s", got)
	}
}

func TestRedactor_SecretKeyValue(t *testing.T) {
	r := NewRedactor()
	msg := "config loaded Secret=abc123xyz"
	got := r.Redact(msg)
	if strings.Contains(got, "abc123xyz") {
		t.Errorf("secret value should be redacted, got: %s", got)
	}
}

func TestRedactor_JWTToken(t *testing.T) {
	r := NewRedactor()
	// A 40-char base64url string that could be a session/JWT token.
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9xyz"
	msg := "session token=" + token
	got := r.Redact(msg)
	if strings.Contains(got, token) {
		t.Errorf("JWT token should be redacted, got: %s", got)
	}
}

func TestRedactor_PlainTextUnchanged(t *testing.T) {
	r := NewRedactor()
	msg := "proxy started on port 443"
	got := r.Redact(msg)
	if got != msg {
		t.Errorf("plain text should not be modified, got: %s", got)
	}
}

// ---- Filter tests ----

func TestFilter_LevelFiltering(t *testing.T) {
	f := Filter{MinLevel: LevelWarn}

	tests := []struct {
		level string
		pass  bool
	}{
		{"error", true},
		{"warn", true},
		{"info", false},
		{"debug", false},
	}

	for _, tc := range tests {
		entry := LogEntry{Level: tc.level, Message: "test message"}
		got := f.matches(entry)
		if got != tc.pass {
			t.Errorf("level=%s: expected matches=%v, got %v", tc.level, tc.pass, got)
		}
	}
}

func TestFilter_LevelError_OnlyError(t *testing.T) {
	f := Filter{MinLevel: LevelError}
	cases := map[string]bool{
		"error": true,
		"warn":  false,
		"info":  false,
		"debug": false,
	}
	for level, want := range cases {
		e := LogEntry{Level: level, Message: "x"}
		if f.matches(e) != want {
			t.Errorf("LevelError filter: level=%s expected %v", level, want)
		}
	}
}

func TestFilter_TextSearch(t *testing.T) {
	f := Filter{MinLevel: LevelDebug, Search: "connect"}

	passing := LogEntry{Level: "info", Message: "new connection established"}
	failing := LogEntry{Level: "info", Message: "service started"}

	if !f.matches(passing) {
		t.Error("entry containing search term should pass")
	}
	if f.matches(failing) {
		t.Error("entry not containing search term should not pass")
	}
}

func TestFilter_TextSearchCaseInsensitive(t *testing.T) {
	f := Filter{MinLevel: LevelDebug, Search: "ERROR"}
	e := LogEntry{Level: "warn", Message: "an error occurred"}
	if !f.matches(e) {
		t.Error("text search should be case-insensitive")
	}
}

func TestFilter_NoSearchPassesAll(t *testing.T) {
	f := Filter{MinLevel: LevelDebug}
	e := LogEntry{Level: "debug", Message: "anything goes"}
	if !f.matches(e) {
		t.Error("empty search filter should pass all entries")
	}
}

// ---- parseJournaldLine tests ----

func TestParseJournaldLine_Valid(t *testing.T) {
	line := `{"__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"6","MESSAGE":"hello world"}`
	entry, ok := parseJournaldLine([]byte(line), "panel")
	if !ok {
		t.Fatal("expected successful parse")
	}
	if entry.Message != "hello world" {
		t.Errorf("unexpected message: %s", entry.Message)
	}
	if entry.Level != "info" {
		t.Errorf("expected level info for PRIORITY 6, got %s", entry.Level)
	}
	if entry.Component != "panel" {
		t.Errorf("unexpected component: %s", entry.Component)
	}
}

func TestParseJournaldLine_Invalid(t *testing.T) {
	_, ok := parseJournaldLine([]byte("not json"), "panel")
	if ok {
		t.Error("expected parse failure for non-JSON input")
	}
}

func TestParseJournaldLine_PriorityMapping(t *testing.T) {
	cases := []struct {
		priority string
		level    string
	}{
		{"0", "error"},
		{"1", "error"},
		{"2", "error"},
		{"3", "error"},
		{"4", "warn"},
		{"5", "info"},
		{"6", "info"},
		{"7", "debug"},
	}
	for _, tc := range cases {
		line := `{"__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"` + tc.priority + `","MESSAGE":"test"}`
		entry, ok := parseJournaldLine([]byte(line), "panel")
		if !ok {
			t.Fatalf("parse failed for priority %s", tc.priority)
		}
		if entry.Level != tc.level {
			t.Errorf("priority %s: expected level %s, got %s", tc.priority, tc.level, entry.Level)
		}
	}
}

// ---- unitForComponent tests ----

func TestUnitForComponent(t *testing.T) {
	cases := map[string]string{
		"panel":     "tgproxy-panel",
		"teleproxy": "teleproxy",
		"sing-box":  "sing-box",
		"nginx":     "nginx",
		"unknown":   "tgproxy-panel",
	}
	for comp, want := range cases {
		got := unitForComponent(comp)
		if got != want {
			t.Errorf("component %q: expected unit %q, got %q", comp, want, got)
		}
	}
}
