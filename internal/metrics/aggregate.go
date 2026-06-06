package metrics

import (
	"fmt"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

// Retention boundaries (exported so tests can override via Retainer fields).
const (
	MinuteRetentionDays = 7
	HourlyRetentionDays = 30
)

// Retainer handles data retention and aggregation.
type Retainer struct {
	DB                  *db.DB
	Now                 func() int64 // returns current Unix timestamp; defaults to time.Now().Unix()
	MinuteRetentionDays int          // 0 → use package constant (7)
	HourlyRetentionDays int          // 0 → use package constant (30)
}

func (r Retainer) now() int64 {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now().Unix()
}

func (r Retainer) minuteDays() int {
	if r.MinuteRetentionDays > 0 {
		return r.MinuteRetentionDays
	}
	return MinuteRetentionDays
}

func (r Retainer) hourlyDays() int {
	if r.HourlyRetentionDays > 0 {
		return r.HourlyRetentionDays
	}
	return HourlyRetentionDays
}

// Run executes the full retention cycle:
//  1. Aggregate traffic_samples older than MinuteRetentionDays into traffic_hourly.
//  2. Delete from traffic_samples rows older than MinuteRetentionDays (after aggregation).
//  3. Delete from traffic_hourly rows older than HourlyRetentionDays.
//
// Each step runs in its own transaction. Run returns the first error encountered.
func (r Retainer) Run() error {
	now := r.now()
	minuteCutoff := now - int64(r.minuteDays())*86400
	hourlyCutoff := now - int64(r.hourlyDays())*86400

	if err := r.AggregateOldSamples(minuteCutoff); err != nil {
		return fmt.Errorf("aggregate old samples: %w", err)
	}
	if err := r.DeleteOldSamples(minuteCutoff); err != nil {
		return fmt.Errorf("delete old samples: %w", err)
	}
	if err := r.DeleteOldHourly(hourlyCutoff); err != nil {
		return fmt.Errorf("delete old hourly: %w", err)
	}
	if err := r.DeleteOldOps(minuteCutoff); err != nil {
		return fmt.Errorf("delete old ops samples: %w", err)
	}
	return nil
}

// DeleteOldOps deletes rows from ops_samples where ts < cutoff. ops_samples is
// kept at minute granularity for the same horizon as traffic_samples.
func (r Retainer) DeleteOldOps(cutoff int64) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`DELETE FROM ops_samples WHERE ts < ?`, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}

// AggregateOldSamples aggregates traffic_samples rows with ts < cutoff
// into traffic_hourly using INSERT OR REPLACE, grouping by (user_label, hour_ts).
// hour_ts = ts - (ts % 3600).
// bytes_in/out = SUM; connections = MAX.
func (r Retainer) AggregateOldSamples(cutoff int64) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.Exec(`
INSERT OR REPLACE INTO traffic_hourly(user_label, hour_ts, bytes_in, bytes_out, connections)
SELECT user_label,
       ts - (ts % 3600) AS hour_ts,
       SUM(bytes_in),
       SUM(bytes_out),
       MAX(connections)
FROM traffic_samples
WHERE ts < ?
GROUP BY user_label, hour_ts
`, cutoff)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteOldSamples deletes rows from traffic_samples where ts < cutoff.
func (r Retainer) DeleteOldSamples(cutoff int64) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.Exec(`DELETE FROM traffic_samples WHERE ts < ?`, cutoff)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteOldHourly deletes rows from traffic_hourly where hour_ts < cutoff.
func (r Retainer) DeleteOldHourly(cutoff int64) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.Exec(`DELETE FROM traffic_hourly WHERE hour_ts < ?`, cutoff)
	if err != nil {
		return err
	}
	return tx.Commit()
}
