package panel

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{10 * 1024, "10 KB"},
		{1024 * 1024, "1.0 MB"},
		{5 * 1024 * 1024, "5.0 MB"},
		{10 * 1024 * 1024, "10 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{uint64(1) << 40, "1.0 TB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.in); got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQuotaPct(t *testing.T) {
	cases := []struct {
		used, total uint64
		want        int
	}{
		{0, 0, 0},
		{100, 0, 0},
		{0, 1000, 0},
		{500, 1000, 50},
		{1000, 1000, 100},
		{1500, 1000, 100},
		{999, 1000, 99},
	}
	for _, c := range cases {
		if got := quotaPct(c.used, c.total); got != c.want {
			t.Errorf("quotaPct(%d,%d) = %d, want %d", c.used, c.total, got, c.want)
		}
	}
}

func TestNextResetIn(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	if got := nextResetIn(0, "daily", now); got != "—" {
		t.Errorf("zero periodStart: got %q want —", got)
	}
	if got := nextResetIn(now.Unix(), "", now); got != "—" {
		t.Errorf("empty period: got %q want —", got)
	}

	startDay := now.Add(-2 * time.Hour).Unix()
	if got := nextResetIn(startDay, "daily", now); got != "resets in 22h 0m" {
		t.Errorf("daily countdown: got %q", got)
	}

	startWeek := now.Add(-2 * 24 * time.Hour).Unix()
	if got := nextResetIn(startWeek, "weekly", now); got != "resets in 5d 0h" {
		t.Errorf("weekly countdown: got %q", got)
	}

	startMonth := now.Add(-3 * 24 * time.Hour).Unix()
	if got := nextResetIn(startMonth, "monthly", now); got != "resets in 27d 0h" {
		t.Errorf("monthly countdown: got %q", got)
	}

	overdue := now.Add(-25 * time.Hour).Unix()
	if got := nextResetIn(overdue, "daily", now); got != "rollover pending" {
		t.Errorf("overdue: got %q", got)
	}

	exactBoundary := now.Add(-24 * time.Hour).Unix()
	if got := nextResetIn(exactBoundary, "daily", now); got != "rollover pending" {
		t.Errorf("boundary: got %q", got)
	}

	startMin := now.Add(-23*time.Hour - 30*time.Minute).Unix()
	if got := nextResetIn(startMin, "daily", now); got != "resets in 30m" {
		t.Errorf("minutes only: got %q", got)
	}
}

func TestUserListDashboardLinkUsesPanelPath(t *testing.T) {
	var buf bytes.Buffer

	userListPage(&buf, nil, "csrf", "", "/p-example/")

	html := buf.String()
	if !strings.Contains(html, `href="/p-example/dashboard"`) {
		t.Fatalf("users page must link back inside panel path:\n%s", html)
	}
	if strings.Contains(html, `href="../"`) {
		t.Fatalf("users page must not use parent-relative dashboard link:\n%s", html)
	}
}
