package metrics

import (
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func insertSample(t *testing.T, d *db.DB, label string, ts, in, out, conns int64) {
	t.Helper()
	_, err := d.Exec(`INSERT INTO traffic_samples(user_label,ts,bytes_in,bytes_out,connections) VALUES(?,?,?,?,?)`,
		label, ts, in, out, conns)
	if err != nil {
		t.Fatal(err)
	}
}

func insertHourly(t *testing.T, d *db.DB, label string, hourTS, in, out, conns int64) {
	t.Helper()
	_, err := d.Exec(`INSERT INTO traffic_hourly(user_label,hour_ts,bytes_in,bytes_out,connections) VALUES(?,?,?,?,?)`,
		label, hourTS, in, out, conns)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAggregateOldSamples(t *testing.T) {
	d := openTestDB(t)
	r := Retainer{DB: d}

	// Three samples for "alice" all in the same hour (hour_ts = 3600).
	insertSample(t, d, "alice", 3600, 100, 200, 5)
	insertSample(t, d, "alice", 3660, 150, 250, 3)
	insertSample(t, d, "alice", 3720, 200, 300, 7)

	if err := r.AggregateOldSamples(10000); err != nil {
		t.Fatalf("AggregateOldSamples: %v", err)
	}

	var label string
	var hourTS, bytesIn, bytesOut, connections int64
	err := d.QueryRow(`SELECT user_label, hour_ts, bytes_in, bytes_out, connections FROM traffic_hourly`).
		Scan(&label, &hourTS, &bytesIn, &bytesOut, &connections)
	if err != nil {
		t.Fatalf("query traffic_hourly: %v", err)
	}

	if label != "alice" {
		t.Errorf("label: got %q, want %q", label, "alice")
	}
	if hourTS != 3600 {
		t.Errorf("hour_ts: got %d, want 3600", hourTS)
	}
	if bytesIn != 450 {
		t.Errorf("bytes_in: got %d, want 450", bytesIn)
	}
	if bytesOut != 750 {
		t.Errorf("bytes_out: got %d, want 750", bytesOut)
	}
	if connections != 7 {
		t.Errorf("connections: got %d, want 7", connections)
	}
}

func TestAggregateOldSamplesMultipleHours(t *testing.T) {
	d := openTestDB(t)
	r := Retainer{DB: d}

	// Samples in two different hours.
	insertSample(t, d, "bob", 3600, 100, 200, 5)  // hour_ts = 3600
	insertSample(t, d, "bob", 7200, 300, 400, 10) // hour_ts = 7200

	if err := r.AggregateOldSamples(20000); err != nil {
		t.Fatalf("AggregateOldSamples: %v", err)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM traffic_hourly WHERE user_label='bob'`).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 rows in traffic_hourly, got %d", count)
	}
}

func TestDeleteOldSamples(t *testing.T) {
	d := openTestDB(t)
	r := Retainer{DB: d}

	insertSample(t, d, "carol", 100, 1, 1, 1)
	insertSample(t, d, "carol", 9999, 2, 2, 2)

	if err := r.DeleteOldSamples(1000); err != nil {
		t.Fatalf("DeleteOldSamples: %v", err)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM traffic_samples`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 row remaining, got %d", count)
	}

	var ts int64
	if err := d.QueryRow(`SELECT ts FROM traffic_samples`).Scan(&ts); err != nil {
		t.Fatal(err)
	}
	if ts != 9999 {
		t.Errorf("expected remaining ts=9999, got %d", ts)
	}
}

func TestDeleteOldHourly(t *testing.T) {
	d := openTestDB(t)
	r := Retainer{DB: d}

	insertHourly(t, d, "dave", 100, 1, 1, 1)
	insertHourly(t, d, "dave", 9999, 2, 2, 2)

	if err := r.DeleteOldHourly(1000); err != nil {
		t.Fatalf("DeleteOldHourly: %v", err)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM traffic_hourly`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 row remaining, got %d", count)
	}

	var hourTS int64
	if err := d.QueryRow(`SELECT hour_ts FROM traffic_hourly`).Scan(&hourTS); err != nil {
		t.Fatal(err)
	}
	if hourTS != 9999 {
		t.Errorf("expected remaining hour_ts=9999, got %d", hourTS)
	}
}

func TestRetainerRunBoundaries(t *testing.T) {
	d := openTestDB(t)

	// Fixed "now": 1_000_000 seconds.
	fixedNow := int64(1_000_000)
	r := Retainer{
		DB:  d,
		Now: func() int64 { return fixedNow },
	}

	minuteCutoff := fixedNow - int64(MinuteRetentionDays)*86400 // 1_000_000 - 604_800 = 395_200

	// Old sample: well before minuteCutoff.
	oldTS := minuteCutoff - 3600
	// Recent sample: well after minuteCutoff.
	recentTS := fixedNow - 60

	insertSample(t, d, "eve", oldTS, 500, 600, 4)
	insertSample(t, d, "eve", recentTS, 100, 200, 2)

	if err := r.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Old sample should be aggregated into traffic_hourly.
	var hourlyCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM traffic_hourly WHERE user_label='eve'`).Scan(&hourlyCount); err != nil {
		t.Fatal(err)
	}
	if hourlyCount != 1 {
		t.Errorf("expected 1 row in traffic_hourly, got %d", hourlyCount)
	}

	// Old sample should be deleted from traffic_samples.
	// Recent sample should remain.
	var samplesCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM traffic_samples WHERE user_label='eve'`).Scan(&samplesCount); err != nil {
		t.Fatal(err)
	}
	if samplesCount != 1 {
		t.Errorf("expected 1 sample remaining, got %d", samplesCount)
	}

	var remainingTS int64
	if err := d.QueryRow(`SELECT ts FROM traffic_samples WHERE user_label='eve'`).Scan(&remainingTS); err != nil {
		t.Fatal(err)
	}
	if remainingTS != recentTS {
		t.Errorf("expected remaining ts=%d (recent), got %d", recentTS, remainingTS)
	}
}

func TestRetainerRunHourlyExpiry(t *testing.T) {
	d := openTestDB(t)

	fixedNow := int64(1_000_000)
	r := Retainer{
		DB:  d,
		Now: func() int64 { return fixedNow },
	}

	hourlyCutoff := fixedNow - int64(HourlyRetentionDays)*86400 // 1_000_000 - 2_592_000 = -1_592_000 (or similar)

	// Insert a row older than 30 days.
	oldHourTS := hourlyCutoff - 7200

	insertHourly(t, d, "frank", oldHourTS, 1000, 2000, 8)

	if err := r.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM traffic_hourly WHERE user_label='frank'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected old hourly row to be deleted, got %d rows", count)
	}
}
