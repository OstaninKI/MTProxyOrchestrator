package metrics_test

import (
	"sort"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func insertTrafficSample(t *testing.T, d *db.DB, label string, ts, in, out, conns int64) {
	t.Helper()
	_, err := d.Exec(
		`INSERT INTO traffic_samples(user_label,ts,bytes_in,bytes_out,connections) VALUES(?,?,?,?,?)`,
		label, ts, in, out, conns)
	if err != nil {
		t.Fatalf("insert sample: %v", err)
	}
}

func TestParsePeriodDefaults(t *testing.T) {
	cases := []struct {
		in   string
		want metrics.Period
	}{
		{"1h", metrics.Period1h},
		{"24h", metrics.Period24h},
		{"7d", metrics.Period7d},
		{"30d", metrics.Period30d},
		{"", metrics.Period24h},
		{"bad", metrics.Period24h},
	}
	for _, tc := range cases {
		got := metrics.ParsePeriod(tc.in)
		if got != tc.want {
			t.Errorf("ParsePeriod(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestQueryTopUsersBasic(t *testing.T) {
	d := openTestDB(t)

	// Fixed "now" at ts=10000. 24h period → cutoff=10000-86400 = negative → all rows included.
	const now int64 = 10000
	nowFn := func() int64 { return now }

	insertTrafficSample(t, d, "alice", 9000, 100, 200, 2)
	insertTrafficSample(t, d, "bob", 9000, 500, 600, 5)
	insertTrafficSample(t, d, "alice", 9100, 50, 50, 1)

	top, err := metrics.QueryTopUsers(d, metrics.Period24h, 5, nowFn)
	if err != nil {
		t.Fatalf("QueryTopUsers: %v", err)
	}
	if len(top) == 0 {
		t.Fatal("expected results, got none")
	}
	// bob has more total traffic (1100) vs alice (400).
	if top[0].UserLabel != "bob" {
		t.Errorf("top[0] = %q, want bob", top[0].UserLabel)
	}
	if top[0].BytesIn != 500 {
		t.Errorf("bob BytesIn = %d, want 500", top[0].BytesIn)
	}
}

func TestQueryTopUsersLimit(t *testing.T) {
	d := openTestDB(t)
	const now int64 = 1_000_000
	nowFn := func() int64 { return now }

	// Insert 5 users.
	users := []string{"u1", "u2", "u3", "u4", "u5"}
	for i, u := range users {
		insertTrafficSample(t, d, u, now-100, int64(i+1)*100, int64(i+1)*100, 1)
	}

	top, err := metrics.QueryTopUsers(d, metrics.Period24h, 3, nowFn)
	if err != nil {
		t.Fatalf("QueryTopUsers: %v", err)
	}
	if len(top) != 3 {
		t.Errorf("expected 3 results, got %d", len(top))
	}
}

func TestQueryTopUsersEmpty(t *testing.T) {
	d := openTestDB(t)
	nowFn := func() int64 { return 1000 }

	top, err := metrics.QueryTopUsers(d, metrics.Period1h, 5, nowFn)
	if err != nil {
		t.Fatalf("QueryTopUsers: %v", err)
	}
	if len(top) != 0 {
		t.Errorf("expected empty, got %d results", len(top))
	}
}

func TestQueryTopUsersExcludesOldData(t *testing.T) {
	d := openTestDB(t)
	const now int64 = 1_000_000
	nowFn := func() int64 { return now }

	// Old sample, outside 1h window.
	insertTrafficSample(t, d, "old", now-7200, 9999, 9999, 10)
	// Recent sample within 1h.
	insertTrafficSample(t, d, "recent", now-100, 1, 1, 1)

	top, err := metrics.QueryTopUsers(d, metrics.Period1h, 5, nowFn)
	if err != nil {
		t.Fatalf("QueryTopUsers: %v", err)
	}
	// Only "recent" should be in results.
	for _, u := range top {
		if u.UserLabel == "old" {
			t.Error("old data should be excluded by 1h window")
		}
	}
	labels := make([]string, len(top))
	for i, u := range top {
		labels[i] = u.UserLabel
	}
	sort.Strings(labels)
	found := sort.SearchStrings(labels, "recent") < len(labels) && labels[sort.SearchStrings(labels, "recent")] == "recent"
	if len(top) > 0 && !found {
		t.Error("recent sample should be in results")
	}
}
