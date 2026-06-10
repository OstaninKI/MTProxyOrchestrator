package quota_test

import (
	"context"
	"errors"
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
	// Period was 25h ago; daily rolls forward exactly one 24h step.
	wantStart := start + 24*3600
	if used != 0 || suspended != 0 || periodStart != wantStart {
		t.Errorf("after rollover: used=%d suspended=%d start=%d (want 0,0,%d)", used, suspended, periodStart, wantStart)
	}
}

func TestRolloverMonthlyAdvancesByCalendarMonth(t *testing.T) {
	// Jan 31 + 1 month must clamp to Feb 28 (2026 is not a leap year), not
	// overflow into March.
	startT := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	end := quota.PeriodEnd(quota.PeriodMonthly, startT.Unix())
	got := time.Unix(end, 0).UTC()
	if got.Year() != 2026 || got.Month() != time.February || got.Day() != 28 {
		t.Errorf("Jan 31 + 1mo = %v, want 2026-02-28", got)
	}

	// Leap year: Jan 31 2024 + 1 month = Feb 29 2024.
	startLeap := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	gotLeap := time.Unix(quota.PeriodEnd(quota.PeriodMonthly, startLeap.Unix()), 0).UTC()
	if gotLeap.Year() != 2024 || gotLeap.Month() != time.February || gotLeap.Day() != 29 {
		t.Errorf("Jan 31 2024 + 1mo = %v, want 2024-02-29", gotLeap)
	}

	// Mar 31 + 1 month = Apr 30, not May 1.
	startMar := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	gotMar := time.Unix(quota.PeriodEnd(quota.PeriodMonthly, startMar.Unix()), 0).UTC()
	if gotMar.Year() != 2026 || gotMar.Month() != time.April || gotMar.Day() != 30 {
		t.Errorf("Mar 31 + 1mo = %v, want 2026-04-30", gotMar)
	}

	// Year boundary: Dec 15 + 1 month = Jan 15 next year.
	startDec := time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)
	gotDec := time.Unix(quota.PeriodEnd(quota.PeriodMonthly, startDec.Unix()), 0).UTC()
	if gotDec.Year() != 2027 || gotDec.Month() != time.January || gotDec.Day() != 15 {
		t.Errorf("Dec 15 + 1mo = %v, want 2027-01-15", gotDec)
	}
}

func TestRolloverMultiPeriodCatchUp(t *testing.T) {
	// Service was down for ~3 daily periods. Rollover should advance one period
	// at a time (not jump straight to now), so period_start lands on the start
	// of the period that contains now.
	d := openDB(t)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	start := now.Add(-3*24*time.Hour - 2*time.Hour).Unix() // 3d2h ago
	insertUser(t, d, "eve", start, "daily", 100, 80)

	s := &quota.Service{DB: d, Now: func() time.Time { return now },
		Audit: func(string, string, string) {}}
	rolled, err := s.RolloverIfDue(context.Background(), "eve")
	if err != nil {
		t.Fatal(err)
	}
	if !rolled {
		t.Fatal("expected rollover")
	}
	var got int64
	if err := d.QueryRow(`SELECT quota_period_start FROM users WHERE label=?`, "eve").Scan(&got); err != nil {
		t.Fatal(err)
	}
	// Three 24h advances from start: start + 3d.
	want := start + 3*24*3600
	if got != want {
		t.Errorf("period_start=%d want %d (start+3d)", got, want)
	}
	// New period must contain now.
	if int64(now.Unix()) < got || int64(now.Unix()) >= got+24*3600 {
		t.Errorf("period_start=%d does not contain now=%d", got, now.Unix())
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

// TestRecalculateRetriesFailedReloadOnNextTick covers the gap where a
// suspension transition commits to the DB but the teleproxy reload fails:
// the next recalculation sees no transition, so without a retry the user's
// secret would stay in the live config indefinitely.
func TestRecalculateRetriesFailedReloadOnNextTick(t *testing.T) {
	d := openDB(t)
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	insertUser(t, d, "dave", now.Add(-24*time.Hour).Unix(), "monthly", 1000, 0)
	insertDaily(t, d, "dave", now.Unix(), 600, 600)

	reloadCalls := 0
	s := &quota.Service{DB: d, Now: func() time.Time { return now },
		Reload: func() error {
			reloadCalls++
			if reloadCalls == 1 {
				return errors.New("teleproxy reload failed")
			}
			return nil
		},
		Audit: func(action, target, detail string) {}}

	if _, _, err := s.Recalculate(context.Background(), "dave"); err == nil {
		t.Fatal("expected error when reload fails on suspension transition")
	}
	if reloadCalls != 1 {
		t.Fatalf("reload calls after first recalculate = %d, want 1", reloadCalls)
	}

	// No state transition this time, but the failed reload must be retried.
	if _, _, err := s.Recalculate(context.Background(), "dave"); err != nil {
		t.Fatalf("second recalculate: %v", err)
	}
	if reloadCalls != 2 {
		t.Fatalf("reload calls = %d, want 2 (failed reload must be retried)", reloadCalls)
	}
}

// TestRecalculateDoesNotReloadWhenNothingPending verifies the retry flag does
// not cause spurious reloads on steady-state recalculations.
func TestRecalculateDoesNotReloadWhenNothingPending(t *testing.T) {
	d := openDB(t)
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	insertUser(t, d, "erin", now.Add(-24*time.Hour).Unix(), "monthly", 1000, 0)
	insertDaily(t, d, "erin", now.Unix(), 1, 1)

	reloadCalls := 0
	s := &quota.Service{DB: d, Now: func() time.Time { return now },
		Reload: func() error { reloadCalls++; return nil },
		Audit:  func(action, target, detail string) {}}

	for i := 0; i < 3; i++ {
		if _, _, err := s.Recalculate(context.Background(), "erin"); err != nil {
			t.Fatal(err)
		}
	}
	if reloadCalls != 0 {
		t.Fatalf("reload calls = %d, want 0 (no transition, no pending retry)", reloadCalls)
	}
}
