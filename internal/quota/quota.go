// Package quota implements per-user traffic quotas with soft warnings, hard
// suspensions, and automatic period rollover. Usage is computed from the
// existing traffic_daily aggregation table in internal/metrics.
package quota

import (
	"context"
	"database/sql"
	"fmt"
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
		return time.Date(t.Year(), t.Month()+1, t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC).Unix()
	default:
		return time.Date(t.Year(), t.Month()+1, t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC).Unix()
	}
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

func (s *Service) loadUser(ctx context.Context, label string) (userQuotaRow, error) {
	var u userQuotaRow
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, label, quota_bytes, quota_period, quota_warn_pct,
		        quota_suspended, quota_period_start, quota_used_bytes, quota_warned
		 FROM users WHERE label=? AND deleted_at IS NULL`, label)
	err := row.Scan(&u.ID, &u.Label, &u.QuotaBytes, &u.QuotaPeriod, &u.QuotaWarnPct,
		&u.QuotaSuspended, &u.QuotaPeriodSt, &u.QuotaUsedBytes, &u.QuotaWarned)
	return u, err
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

// usageSince returns the sum of bytes_in + bytes_out from traffic_daily for
// the user since fromTS.
func (s *Service) usageSince(ctx context.Context, label string, fromTS int64) (int64, error) {
	var total sql.NullInt64
	err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(bytes_in)+SUM(bytes_out),0)
		 FROM traffic_daily WHERE user_label=? AND day_ts >= ?`,
		label, fromTS).Scan(&total)
	if err != nil {
		return 0, err
	}
	return total.Int64, nil
}

// RolloverIfDue advances the period if the current period has ended.
// Returns whether a rollover happened.
func (s *Service) RolloverIfDue(ctx context.Context, label string) (bool, error) {
	u, err := s.loadUser(ctx, label)
	if err != nil {
		return false, err
	}
	now := s.now().Unix()
	if u.QuotaPeriodSt == 0 {
		_, err := s.DB.ExecContext(ctx,
			`UPDATE users SET quota_period_start=? WHERE id=?`, now, u.ID)
		return false, err
	}
	end := PeriodEnd(u.QuotaPeriod, u.QuotaPeriodSt)
	if now < end {
		return false, nil
	}
	wasSuspended := u.QuotaSuspended
	_, err = s.DB.ExecContext(ctx,
		`UPDATE users SET quota_used_bytes=0, quota_warned=0, quota_suspended=0,
		                  quota_period_start=? WHERE id=?`, now, u.ID)
	if err != nil {
		return false, err
	}
	s.Audit("quota_rollover", label, fmt.Sprintf("period=%s", u.QuotaPeriod))
	if wasSuspended && s.Reload != nil {
		if err := s.Reload(); err != nil {
			return true, err
		}
	}
	return true, nil
}

// Recalculate reads traffic_daily, updates cached usage, and toggles
// suspension when the hard limit is crossed. Emits a one-shot audit warning
// when the soft warn threshold is crossed within the current period.
func (s *Service) Recalculate(ctx context.Context, label string) (int64, bool, error) {
	u, err := s.loadUser(ctx, label)
	if err != nil {
		return 0, false, err
	}
	used, err := s.usageSince(ctx, label, u.QuotaPeriodSt)
	if err != nil {
		return 0, u.QuotaSuspended, err
	}

	newSuspended := u.QuotaSuspended
	if u.QuotaBytes > 0 && used >= u.QuotaBytes {
		newSuspended = true
	}

	newWarned := u.QuotaWarned
	if u.QuotaBytes > 0 && !u.QuotaWarned && u.QuotaWarnPct > 0 {
		threshold := u.QuotaBytes * int64(u.QuotaWarnPct) / 100
		if used >= threshold {
			newWarned = true
			s.Audit("quota_warning", label,
				fmt.Sprintf("used=%d quota=%d pct=%d", used, u.QuotaBytes, u.QuotaWarnPct))
		}
	}

	_, err = s.DB.ExecContext(ctx,
		`UPDATE users SET quota_used_bytes=?, quota_suspended=?, quota_warned=? WHERE id=?`,
		used, newSuspended, newWarned, u.ID)
	if err != nil {
		return used, newSuspended, err
	}

	if newSuspended != u.QuotaSuspended {
		if newSuspended {
			s.Audit("quota_suspend", label,
				fmt.Sprintf("used=%d quota=%d", used, u.QuotaBytes))
		} else {
			s.Audit("quota_unsuspend", label, "")
		}
		if s.Reload != nil {
			if err := s.Reload(); err != nil {
				return used, newSuspended, err
			}
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
