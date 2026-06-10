// Package quota implements per-user traffic quotas with soft warnings, hard
// suspensions, and automatic period rollover. Usage is computed from the
// existing traffic_daily aggregation table in internal/metrics.
package quota

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/audit"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

// Period values stored in users.quota_period.
const (
	PeriodDaily   = "daily"
	PeriodWeekly  = "weekly"
	PeriodMonthly = "monthly"
)

// ReloadFunc is invoked after a user's suspended state transitions so the
// caller can rebuild and reload the teleproxy config without that user.
type ReloadFunc func() error

// AuditFunc records a quota event. Defaults to writing to audit_log.
type AuditFunc func(action, target, detail string)

// Service performs quota recalculation and period rollover.
type Service struct {
	DB     *db.DB
	Now    func() time.Time
	Reload ReloadFunc
	Audit  AuditFunc

	// reloadPending is set when a suspension transition committed to the DB
	// but the subsequent Reload failed. The next recalculation retries the
	// reload even without a new transition, so the live teleproxy config
	// cannot stay out of sync with the DB indefinitely.
	mu            sync.Mutex
	reloadPending bool
}

// runReload invokes Reload and tracks failures for retry on the next tick.
func (s *Service) runReload() error {
	if s.Reload == nil {
		return nil
	}
	err := s.Reload()
	s.mu.Lock()
	s.reloadPending = err != nil
	s.mu.Unlock()
	return err
}

// takeReloadPending reports whether a previously failed reload needs a retry.
func (s *Service) takeReloadPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reloadPending
}

// NewService constructs a Service with sane defaults. dbAudit is used when
// Audit is not set explicitly.
func NewService(d *db.DB, reload ReloadFunc) *Service {
	s := &Service{
		DB:     d,
		Now:    time.Now,
		Reload: reload,
	}
	s.Audit = func(action, target, detail string) {
		audit.Log(d, 0, action, target, detail, "") //nolint:errcheck
	}
	return s
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// PeriodEnd returns the unix timestamp at which the period that started at
// startTS ends.
func PeriodEnd(period string, startTS int64) int64 {
	if startTS <= 0 {
		return 0
	}
	t := time.Unix(startTS, 0).UTC()
	switch period {
	case PeriodDaily:
		return t.Add(24 * time.Hour).Unix()
	case PeriodWeekly:
		return t.Add(7 * 24 * time.Hour).Unix()
	case PeriodMonthly:
		return addMonthClamped(t).Unix()
	default:
		return addMonthClamped(t).Unix()
	}
}

// addMonthClamped advances t by exactly one calendar month, clamping the day
// to the last valid day of the next month so that Jan 31 + 1 month = Feb 28
// (or Feb 29 in leap years), not Mar 3.
func addMonthClamped(t time.Time) time.Time {
	y, m, d := t.Date()
	nextY, nextM := y, m+1
	if nextM > time.December {
		nextY, nextM = y+1, time.January
	}
	// Last day of nextM: day 0 of the month after nextM.
	last := time.Date(nextY, nextM+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if d > last {
		d = last
	}
	return time.Date(nextY, nextM, d, t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

// userQuotaRow is the minimal shape we read for quota work.
type userQuotaRow struct {
	ID             int64
	Label          string
	QuotaBytes     int64
	QuotaPeriod    string
	QuotaWarnPct   int
	QuotaSuspended bool
	QuotaPeriodSt  int64
	QuotaUsedBytes int64
	QuotaWarned    bool
}

func (s *Service) listUsers(ctx context.Context) ([]userQuotaRow, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, label, quota_bytes, quota_period, quota_warn_pct,
		        quota_suspended, quota_period_start, quota_used_bytes, quota_warned
		 FROM users WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []userQuotaRow
	for rows.Next() {
		var u userQuotaRow
		if err := rows.Scan(&u.ID, &u.Label, &u.QuotaBytes, &u.QuotaPeriod, &u.QuotaWarnPct,
			&u.QuotaSuspended, &u.QuotaPeriodSt, &u.QuotaUsedBytes, &u.QuotaWarned); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// RolloverIfDue advances the period while the current period has ended.
// Multiple elapsed periods (after a long downtime) are processed one at a time
// so period_start lands at the start of the period that contains now, not
// shifted by however long the service was down. Returns whether at least one
// rollover happened.
func (s *Service) RolloverIfDue(ctx context.Context, label string) (bool, error) {
	tx, err := s.DB.BeginImmediate(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var (
		id          int64
		period      string
		periodStart int64
		suspended   bool
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, quota_period, quota_period_start, quota_suspended
		 FROM users WHERE label=? AND deleted_at IS NULL`, label).
		Scan(&id, &period, &periodStart, &suspended)
	if err != nil {
		return false, err
	}
	now := s.now().Unix()
	if periodStart == 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE users SET quota_period_start=? WHERE id=?`, now, id); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}

	rolled := false
	wasSuspended := suspended
	cur := periodStart
	for {
		end := PeriodEnd(period, cur)
		if end <= 0 || now < end {
			break
		}
		cur = end
		rolled = true
	}
	if !rolled {
		return false, tx.Commit(ctx)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET quota_used_bytes=0, quota_warned=0, quota_suspended=0,
		                  quota_period_start=? WHERE id=?`, cur, id); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	s.Audit("quota_rollover", label, fmt.Sprintf("period=%s", period))
	if wasSuspended {
		if err := s.runReload(); err != nil {
			return true, err
		}
	}
	return true, nil
}

// Recalculate reads traffic_daily, updates cached usage, and toggles
// suspension when the hard limit is crossed. Emits a one-shot audit warning
// when the soft warn threshold is crossed within the current period.
// The state read and write happen inside a BEGIN IMMEDIATE transaction so an
// admin handler that just toggled suspension or quota cannot be clobbered by
// a concurrent tick.
func (s *Service) Recalculate(ctx context.Context, label string) (int64, bool, error) {
	tx, err := s.DB.BeginImmediate(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var u userQuotaRow
	err = tx.QueryRowContext(ctx,
		`SELECT id, label, quota_bytes, quota_period, quota_warn_pct,
		        quota_suspended, quota_period_start, quota_used_bytes, quota_warned
		 FROM users WHERE label=? AND deleted_at IS NULL`, label).
		Scan(&u.ID, &u.Label, &u.QuotaBytes, &u.QuotaPeriod, &u.QuotaWarnPct,
			&u.QuotaSuspended, &u.QuotaPeriodSt, &u.QuotaUsedBytes, &u.QuotaWarned)
	if err != nil {
		return 0, false, err
	}

	var totalNull sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(bytes_in)+SUM(bytes_out),0)
		 FROM traffic_daily WHERE user_label=? AND day_ts >= ?`,
		label, u.QuotaPeriodSt).Scan(&totalNull); err != nil {
		return 0, u.QuotaSuspended, err
	}
	used := totalNull.Int64

	newSuspended := u.QuotaSuspended
	if u.QuotaBytes > 0 && used >= u.QuotaBytes {
		newSuspended = true
	}

	newWarned := u.QuotaWarned
	emitWarning := false
	if u.QuotaBytes > 0 && !u.QuotaWarned && u.QuotaWarnPct > 0 {
		threshold := u.QuotaBytes * int64(u.QuotaWarnPct) / 100
		if used >= threshold {
			newWarned = true
			emitWarning = true
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET quota_used_bytes=?, quota_suspended=?, quota_warned=? WHERE id=?`,
		used, newSuspended, newWarned, u.ID); err != nil {
		return used, newSuspended, err
	}
	if err := tx.Commit(ctx); err != nil {
		return used, newSuspended, err
	}

	if emitWarning {
		s.Audit("quota_warning", label,
			fmt.Sprintf("used=%d quota=%d pct=%d", used, u.QuotaBytes, u.QuotaWarnPct))
	}

	if newSuspended != u.QuotaSuspended {
		if newSuspended {
			s.Audit("quota_suspend", label,
				fmt.Sprintf("used=%d quota=%d", used, u.QuotaBytes))
		} else {
			s.Audit("quota_unsuspend", label, "")
		}
		if err := s.runReload(); err != nil {
			return used, newSuspended, err
		}
	} else if s.takeReloadPending() {
		// A previous suspension transition committed but its reload failed;
		// retry so the rendered config catches up with the DB state.
		if err := s.runReload(); err != nil {
			return used, newSuspended, err
		}
	}
	return used, newSuspended, nil
}

// RunPeriodic ticks every interval, rolls over and recalculates every user.
// Returns when ctx is cancelled.
func (s *Service) RunPeriodic(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	s.tickAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tickAll(ctx)
		}
	}
}

func (s *Service) tickAll(ctx context.Context) {
	users, err := s.listUsers(ctx)
	if err != nil {
		return
	}
	for _, u := range users {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.RolloverIfDue(ctx, u.Label); err != nil {
			continue
		}
		s.Recalculate(ctx, u.Label) //nolint:errcheck
	}
}
