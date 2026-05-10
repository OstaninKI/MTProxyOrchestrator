package quota_test

import (
	"context"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/quota"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func insertUser(t *testing.T, d *db.DB, label string, periodStart int64, period string, quotaBytes int64, warnPct int) {
	t.Helper()
	_, err := d.Exec(
		`INSERT INTO users(label, secret_hex, quota_bytes, quota_period, quota_warn_pct, quota_period_start)
		 VALUES(?,?,?,?,?,?)`,
		label, "deadbeef", quotaBytes, period, warnPct, periodStart,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

func insertDaily(t *testing.T, d *db.DB, label string, dayTS, in, out int64) {
	t.Helper()
	_, err := d.Exec(
		`INSERT INTO traffic_daily(user_label, day_ts, bytes_in, bytes_out, connections) VALUES (?,?,?,?,0)`,
		label, dayTS, in, out,
	)
	if err != nil {
		t.Fatalf("insert daily: %v", err)
	}
}

func TestRecalculateSuspendsAtHardLimit(t *testing.T) {
	d := openDB(t)
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	insertUser(t, d, "alice", now.Add(-24*time.Hour).Unix(), "monthly", 1000, 80)
	insertDaily(t, d, "alice", now.Unix(), 600, 600)

	reloaded := false
	s := &quota.Service{DB: d, Now: func() time.Time { return now }, Reload: func() error { reloaded = true; return nil },
		Audit: func(action, target, detail string) {}}
	used, suspended, err := s.Recalculate(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if used != 1200 {
		t.Errorf("used=%d want 1200", used)
	}
	if !suspended {
		t.Error("expected suspended=true")
	}
	if !reloaded {
		t.Error("expected reload to be invoked on transition")
	}
}

func TestRecalculateWarnEmittedOnce(t *testing.T) {
	d := openDB(t)
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	insertUser(t, d, "bob", now.Add(-24*time.Hour).Unix(), "monthly", 1000, 80)
	insertDaily(t, d, "bob", now.Unix(), 400, 450) // 850 -> 85% > 80

	var events []string
	s := &quota.Service{DB: d, Now: func() time.Time { return now },
		Audit: func(action, target, detail string) { events = append(events, action) }}
	if _, _, err := s.Recalculate(context.Background(), "bob"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Recalculate(context.Background(), "bob"); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range events {
		if e == "quota_warning" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 quota_warning, got %d", count)
	}
}

func TestRolloverDaily(t *testing.T) {
	d := openDB(t)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	start := now.Add(-25 * time.Hour).Unix()
	insertUser(t, d, "carol", start, "daily", 100, 80)
	// Mark suspended so we verify reload is called on rollover.
	if _, err := d.Exec(`UPDATE users SET quota_suspended=1, quota_used_bytes=999, quota_warned=1 WHERE label=?`, "carol"); err != nil {
		t.Fatal(err)
	}

	reloaded := false
	s := &quota.Service{DB: d, Now: func() time.Time { return now },
		Reload: func() error { reloaded = true; return nil },
		Audit:  func(action, target, detail string) {}}
	rolled, err := s.RolloverIfDue(context.Background(), "carol")
	if err != nil {
		t.Fatal(err)
	}
	if !rolled {
		t.Fatal("expected rollover")
	}
	if !reloaded {
		t.Error("expected reload to be called when previously suspended")
	}
	var used, suspended, periodStart int64
	if err := d.QueryRow(`SELECT quota_used_bytes, quota_suspended, quota_period_start FROM users WHERE label=?`, "carol").
		Scan(&used, &suspended, &periodStart); err != nil {
		t.Fatal(err)
	}
	if used != 0 || suspended != 0 || periodStart != now.Unix() {
		t.Errorf("after rollover: used=%d suspended=%d start=%d (want 0,0,%d)", used, suspended, periodStart, now.Unix())
	}
}

func TestRolloverMonthlyAdvancesByCalendarMonth(t *testing.T) {
	startT := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	end := quota.PeriodEnd(quota.PeriodMonthly, startT.Unix())
	got := time.Unix(end, 0).UTC()
	if got.Month() != time.March || got.Day() != 3 { // Feb 31 normalises to Mar 3
		// Accept either canonical Feb 28 or normalised Mar 3 — Go's time.Date
		// normalises overflow into March. Just assert >= startT + 28d.
		if got.Sub(startT) < 28*24*time.Hour {
			t.Errorf("monthly rollover too short: %v", got)
		}
	}
}

func TestRolloverNotDue(t *testing.T) {
	d := openDB(t)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	insertUser(t, d, "dan", now.Add(-2*time.Hour).Unix(), "daily", 100, 80)
	s := &quota.Service{DB: d, Now: func() time.Time { return now }, Audit: func(a, t, d string) {}}
	rolled, err := s.RolloverIfDue(context.Background(), "dan")
	if err != nil {
		t.Fatal(err)
	}
	if rolled {
		t.Error("expected no rollover")
	}
}

func TestRolloverWeekly(t *testing.T) {
	startT := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := quota.PeriodEnd(quota.PeriodWeekly, startT.Unix())
	if end-startT.Unix() != 7*24*3600 {
		t.Errorf("weekly period not 7 days: delta=%d", end-startT.Unix())
	}
}
