package panel

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
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

	userListPage(&buf, nil, "csrf", "", "", "/p-example/")

	html := buf.String()
	if !strings.Contains(html, `href="/p-example/dashboard"`) {
		t.Fatalf("users page must link back inside panel path:\n%s", html)
	}
	if strings.Contains(html, `href="../"`) {
		t.Fatalf("users page must not use parent-relative dashboard link:\n%s", html)
	}
}

func TestUserListShowsTrafficBreakdownAndConnectionStatus(t *testing.T) {
	users := []UserRow{
		{
			Label:                  "alice",
			Enabled:                true,
			QuotaBytes:             0,
			TrafficUploadedBytes:   1024,
			TrafficDownloadedBytes: 2 * 1024 * 1024,
			TrafficTotalBytes:      2*1024*1024 + 1024,
			ActiveConnections:      2,
			ConnectionStatus:       UserConnectionOnline,
			CreatedAt:              time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC),
		},
		{
			Label:            "bob",
			Enabled:          true,
			ConnectionStatus: UserConnectionNever,
			CreatedAt:        time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
		},
	}

	var buf bytes.Buffer
	userListPage(&buf, users, "csrf", "", "", "/p-example/")
	html := buf.String()

	for _, want := range []string{
		`class="grid-12"`,
		`data-users-page`,
		`data-users-role="search"`,
		`data-users-role="status"`,
		`data-users-role="sort"`,
		`data-user-row`,
		`class="row-menu"`,
		`data-users-select-all`,
		`data-users-sort-key="traffic-desc"`,
		`class="user-avatar"`,
		"All users",
		"Online",
		"Offline",
		"Suspended",
		"Total",
		"2.0 MB",
		"1.0 KB",
		"online now",
		"not connected",
		`class="quota-form"`,
		`class="quota-number quota-gb"`,
		`inputmode="decimal"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("users page missing %q:\n%s", want, html)
		}
	}
}

func TestUserRepoListUsesRecordedTrafficForUnlimitedUsers(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	if _, err := d.Exec(
		`INSERT INTO users(label, secret_hex, quota_used_bytes) VALUES(?,?,0)`,
		"alice", "deadbeef",
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO traffic_daily(user_label, day_ts, bytes_in, bytes_out, connections) VALUES(?,?,?,?,?)`,
		"alice", int64(0), int64(150), int64(350), int64(2),
	); err != nil {
		t.Fatalf("insert traffic: %v", err)
	}

	users, err := UserRepo{DB: d}.List()
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("len(users) = %d, want 1", len(users))
	}
	if users[0].QuotaUsedBytes != 500 {
		t.Fatalf("QuotaUsedBytes = %d, want 500", users[0].QuotaUsedBytes)
	}
}

func TestUserRepoListReturnsUsageAndStatus(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	if _, err := d.Exec(
		`INSERT INTO users(label, secret_hex) VALUES
		    ('alice', 'deadbeef'),
		    ('bob', 'feedface'),
		    ('carol', 'cafebabe')`,
	); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO traffic_daily(user_label, day_ts, bytes_in, bytes_out, connections) VALUES
		    ('alice', 0, 100, 300, 2),
		    ('bob', 0, 50, 70, 0)`,
	); err != nil {
		t.Fatalf("insert daily traffic: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO traffic_samples(user_label, ts, bytes_in, bytes_out, connections) VALUES
		    ('alice', 1000, 100, 300, 2),
		    ('bob', 1000, 50, 70, 0)`,
	); err != nil {
		t.Fatalf("insert samples: %v", err)
	}

	users, err := UserRepo{DB: d}.List()
	if err != nil {
		t.Fatalf("list users: %v", err)
	}

	byLabel := map[string]UserRow{}
	for _, user := range users {
		byLabel[user.Label] = user
	}
	alice := byLabel["alice"]
	if alice.TrafficUploadedBytes != 100 || alice.TrafficDownloadedBytes != 300 || alice.TrafficTotalBytes != 400 {
		t.Fatalf("alice traffic = upload %d download %d total %d, want 100/300/400",
			alice.TrafficUploadedBytes, alice.TrafficDownloadedBytes, alice.TrafficTotalBytes)
	}
	if alice.ActiveConnections != 2 || alice.ConnectionStatus != UserConnectionOnline {
		t.Fatalf("alice status = %q with %d connections, want online with 2",
			alice.ConnectionStatus, alice.ActiveConnections)
	}
	if bob := byLabel["bob"]; bob.ConnectionStatus != UserConnectionOffline {
		t.Fatalf("bob status = %q, want offline", bob.ConnectionStatus)
	}
	if carol := byLabel["carol"]; carol.ConnectionStatus != UserConnectionNever {
		t.Fatalf("carol status = %q, want never", carol.ConnectionStatus)
	}
}
