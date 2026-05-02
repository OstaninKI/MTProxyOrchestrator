package db_test

import (
	"strings"
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

func TestTrafficSamplesTableExists(t *testing.T) {
	d := openTestDB(t)
	_, err := d.Exec(`INSERT INTO traffic_samples (user_label, ts, bytes_in, bytes_out, connections) VALUES (?, ?, ?, ?, ?)`,
		"alice", 1000, 100, 200, 5)
	if err != nil {
		t.Fatalf("insert into traffic_samples: %v", err)
	}
	var label string
	var ts, bytesIn, bytesOut, connections int64
	err = d.QueryRow(`SELECT user_label, ts, bytes_in, bytes_out, connections FROM traffic_samples LIMIT 1`).
		Scan(&label, &ts, &bytesIn, &bytesOut, &connections)
	if err != nil {
		t.Fatalf("select from traffic_samples: %v", err)
	}
	if label != "alice" || ts != 1000 || bytesIn != 100 || bytesOut != 200 || connections != 5 {
		t.Errorf("unexpected row values: label=%q ts=%d bytes_in=%d bytes_out=%d connections=%d",
			label, ts, bytesIn, bytesOut, connections)
	}
}

func TestTrafficHourlyTableExists(t *testing.T) {
	d := openTestDB(t)
	_, err := d.Exec(`INSERT INTO traffic_hourly (user_label, hour_ts, bytes_in, bytes_out, connections) VALUES (?, ?, ?, ?, ?)`,
		"bob", 3600, 50, 150, 3)
	if err != nil {
		t.Fatalf("insert into traffic_hourly: %v", err)
	}
	var label string
	var hourTs, bytesIn, bytesOut, connections int64
	err = d.QueryRow(`SELECT user_label, hour_ts, bytes_in, bytes_out, connections FROM traffic_hourly LIMIT 1`).
		Scan(&label, &hourTs, &bytesIn, &bytesOut, &connections)
	if err != nil {
		t.Fatalf("select from traffic_hourly: %v", err)
	}
	if label != "bob" || hourTs != 3600 || bytesIn != 50 || bytesOut != 150 || connections != 3 {
		t.Errorf("unexpected row values: label=%q hour_ts=%d bytes_in=%d bytes_out=%d connections=%d",
			label, hourTs, bytesIn, bytesOut, connections)
	}
}

func TestTrafficDailyTableExists(t *testing.T) {
	d := openTestDB(t)
	_, err := d.Exec(`INSERT INTO traffic_daily (user_label, day_ts, bytes_in, bytes_out, connections) VALUES (?, ?, ?, ?, ?)`,
		"carol", 86400, 1000, 2000, 10)
	if err != nil {
		t.Fatalf("insert into traffic_daily: %v", err)
	}
	var label string
	var dayTs, bytesIn, bytesOut, connections int64
	err = d.QueryRow(`SELECT user_label, day_ts, bytes_in, bytes_out, connections FROM traffic_daily LIMIT 1`).
		Scan(&label, &dayTs, &bytesIn, &bytesOut, &connections)
	if err != nil {
		t.Fatalf("select from traffic_daily: %v", err)
	}
	if label != "carol" || dayTs != 86400 || bytesIn != 1000 || bytesOut != 2000 || connections != 10 {
		t.Errorf("unexpected row values: label=%q day_ts=%d bytes_in=%d bytes_out=%d connections=%d",
			label, dayTs, bytesIn, bytesOut, connections)
	}
}

func TestIdxUserTimeExists(t *testing.T) {
	d := openTestDB(t)
	rows, err := d.Query(`EXPLAIN QUERY PLAN SELECT * FROM traffic_samples WHERE user_label=? AND ts>?`, "alice", 0)
	if err != nil {
		t.Fatalf("explain query plan: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan explain row: %v", err)
		}
		if strings.Contains(detail, "idx_user_time") {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}
	if !found {
		t.Error("expected idx_user_time to appear in EXPLAIN QUERY PLAN output")
	}
}

func TestMigrationsIdempotentTraffic(t *testing.T) {
	for i := 0; i < 2; i++ {
		d, err := db.Open(":memory:")
		if err != nil {
			t.Fatalf("open db iteration %d: %v", i, err)
		}
		var count int
		if err := d.QueryRow(`SELECT COUNT(*) FROM migrations`).Scan(&count); err != nil {
			d.Close()
			t.Fatalf("query migrations iteration %d: %v", i, err)
		}
		if count != 4 {
			d.Close()
			t.Errorf("iteration %d: expected 4 migrations recorded, got %d", i, count)
		}
		d.Close()
	}
}
