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

func insertTrafficHourly(t *testing.T, d *db.DB, label string, hourTS, in, out, conns int64) {
	t.Helper()
	_, err := d.Exec(
		`INSERT INTO traffic_hourly(user_label,hour_ts,bytes_in,bytes_out,connections) VALUES(?,?,?,?,?)`,
		label, hourTS, in, out, conns)
	if err != nil {
		t.Fatalf("insert hourly: %v", err)
	}
}

func TestQueryTrafficSeriesBucketsSamples(t *testing.T) {
	d := openTestDB(t)
	const now int64 = 1_700_000_000

	insertTrafficSample(t, d, "alice", now-120, 10, 4, 1)
	insertTrafficSample(t, d, "bob", now-60, 20, 6, 2)

	series, err := metrics.QueryTrafficSeries(d, metrics.Period1h, 60, func() int64 { return now })
	if err != nil {
		t.Fatalf("QueryTrafficSeries: %v", err)
	}
	if len(series) != 60 {
		t.Fatalf("len(series) = %d, want 60", len(series))
	}

	if series[57].BytesIn != 0 || series[57].BytesOut != 0 || series[57].Connections != 0 {
		t.Fatalf("bucket[57] = %+v, want zero-filled gap", series[57])
	}
	if series[58].BytesIn != 10 || series[58].BytesOut != 4 || series[58].Connections != 1 {
		t.Fatalf("bucket[58] = %+v, want alice sample", series[58])
	}
	last := series[len(series)-1]
	if last.BytesIn != 20 || last.BytesOut != 6 || last.Connections != 2 {
		t.Fatalf("last bucket = %+v, want latest sample totals", last)
	}
}

func TestQueryTrafficSeriesUsesHourlyDataFor30d(t *testing.T) {
	d := openTestDB(t)
	const now int64 = 1_700_000_000

	insertTrafficHourly(t, d, "alice", now-25*24*3600, 100, 40, 3)
	insertTrafficHourly(t, d, "bob", now-10*24*3600, 250, 80, 5)

	series, err := metrics.QueryTrafficSeries(d, metrics.Period30d, 60, func() int64 { return now })
	if err != nil {
		t.Fatalf("QueryTrafficSeries: %v", err)
	}
	if len(series) != 60 {
		t.Fatalf("len(series) = %d, want 60", len(series))
	}

	var foundAlice, foundBob bool
	for _, bucket := range series {
		if bucket.BytesIn == 100 && bucket.BytesOut == 40 && bucket.Connections == 3 {
			foundAlice = true
		}
		if bucket.BytesIn == 250 && bucket.BytesOut == 80 && bucket.Connections == 5 {
			foundBob = true
		}
	}
	if !foundAlice {
		t.Fatal("expected hourly alice bucket in 30d series")
	}
	if !foundBob {
		t.Fatal("expected hourly bob bucket in 30d series")
	}
}

// TestQueryUserTrafficSeriesReturnsMostRecent pins the latent bug fix: the
// query must return the most-recent N samples, not the oldest N. The previous
// `ORDER BY ts ASC LIMIT N` silently returned the oldest rows.
func TestQueryUserTrafficSeriesReturnsMostRecent(t *testing.T) {
	d := openTestDB(t)
	// 5 samples, ask for 3; the newest 3 (ts 30,40,50) must come back.
	insertTrafficSample(t, d, "alice", 10, 1, 1, 1)
	insertTrafficSample(t, d, "alice", 20, 2, 2, 2)
	insertTrafficSample(t, d, "alice", 30, 3, 3, 3)
	insertTrafficSample(t, d, "alice", 40, 4, 4, 4)
	insertTrafficSample(t, d, "alice", 50, 5, 5, 5)

	series, err := metrics.QueryUserTrafficSeries(d, "alice", 3)
	if err != nil {
		t.Fatalf("QueryUserTrafficSeries: %v", err)
	}
	if len(series) != 3 {
		t.Fatalf("got %d buckets, want 3", len(series))
	}
	// Ascending order, newest 3.
	want := []int64{30, 40, 50}
	for i, b := range series {
		if b.TS != want[i] {
			t.Errorf("series[%d].TS = %d, want %d", i, b.TS, want[i])
		}
	}
	if series[0].BytesIn != 3 {
		t.Errorf("first bucket BytesIn = %d, want 3 (oldest was wrongly returned)", series[0].BytesIn)
	}
}

// TestQueryUsersTrafficSeries verifies the batch query returns the newest N
// samples per user in a single round-trip, grouped by label.
func TestQueryUsersTrafficSeries(t *testing.T) {
	d := openTestDB(t)
	// alice: 3 samples, ask for 2 → newest two (20,30).
	insertTrafficSample(t, d, "alice", 10, 1, 1, 1)
	insertTrafficSample(t, d, "alice", 20, 2, 2, 2)
	insertTrafficSample(t, d, "alice", 30, 3, 3, 3)
	// bob: 2 samples, ask for 2 → both (5,6).
	insertTrafficSample(t, d, "bob", 5, 10, 10, 10)
	insertTrafficSample(t, d, "bob", 6, 20, 20, 20)

	out, err := metrics.QueryUsersTrafficSeries(d, []string{"alice", "bob"}, 2)
	if err != nil {
		t.Fatalf("QueryUsersTrafficSeries: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d users, want 2", len(out))
	}
	if len(out["alice"]) != 2 {
		t.Fatalf("alice: got %d buckets, want 2", len(out["alice"]))
	}
	if out["alice"][0].TS != 20 || out["alice"][1].TS != 30 {
		t.Fatalf("alice series = %v, want [20 30]", []int64{out["alice"][0].TS, out["alice"][1].TS})
	}
	if len(out["bob"]) != 2 {
		t.Fatalf("bob: got %d buckets, want 2", len(out["bob"]))
	}
}

// TestQueryUsersTrafficSeriesMostRecent asserts the batch query keeps the
// newest N per user (not the oldest), mirroring the single-user fix.
func TestQueryUsersTrafficSeriesMostRecent(t *testing.T) {
	d := openTestDB(t)
	for ts := int64(1); ts <= 4; ts++ {
		insertTrafficSample(t, d, "alice", ts*100, ts, ts, ts)
	}
	insertTrafficSample(t, d, "bob", 900, 9, 9, 9)

	out, err := metrics.QueryUsersTrafficSeries(d, []string{"alice", "bob"}, 2)
	if err != nil {
		t.Fatalf("QueryUsersTrafficSeries: %v", err)
	}
	// alice has 4 samples; only the newest 2 (ts 300,400) must return.
	if len(out["alice"]) != 2 {
		t.Fatalf("alice: got %d, want 2", len(out["alice"]))
	}
	if out["alice"][0].TS != 300 || out["alice"][1].TS != 400 {
		t.Fatalf("alice series = %v, want newest [300 400]", []int64{out["alice"][0].TS, out["alice"][1].TS})
	}
}

// TestQueryUsersTrafficSeriesEmptyLabels confirms the no-users case is safe.
func TestQueryUsersTrafficSeriesEmptyLabels(t *testing.T) {
	d := openTestDB(t)
	out, err := metrics.QueryUsersTrafficSeries(d, nil, 30)
	if err != nil {
		t.Fatalf("QueryUsersTrafficSeries(nil): %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("got %d entries, want 0", len(out))
	}
}
