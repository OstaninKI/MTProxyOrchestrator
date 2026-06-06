package metrics

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

// ScrapeSource is a function that returns current samples.
// Matches Scraper.Scrape signature for easy injection.
type ScrapeSource func() ([]Sample, error)

// StoreFn writes samples to persistent storage.
type StoreFn func(samples []Sample, ts int64) error

// NowFn returns current Unix timestamp (seconds); injectable for tests.
type NowFn func() int64

// Sampler runs periodic metric collection.
//
// When SnapshotSource is set it is preferred over Source: it yields the full
// snapshot, so per-user samples and global operational counters come from a
// single scrape. OpsStore, when set, persists the operational counters.
type Sampler struct {
	Source         ScrapeSource
	SnapshotSource SnapshotSource
	Store          StoreFn
	OpsStore       OpsStoreFn
	Now            NowFn
	Interval       time.Duration // default 60s when zero
}

// Run collects metrics on every Interval tick until ctx is cancelled.
// Errors from Source and Store are logged but do not stop the loop.
// Run returns when ctx is done.
func (s Sampler) Run(ctx context.Context) error {
	interval := s.Interval
	if interval == 0 {
		interval = 60 * time.Second
	}

	tick := func() {
		var (
			snapshot Snapshot
			samples  []Sample
			err      error
		)
		if s.SnapshotSource != nil {
			snapshot, err = s.SnapshotSource()
			samples = snapshot.Samples
		} else {
			samples, err = s.Source()
		}
		if err != nil {
			slog.Error("metrics scrape failed", "err", err)
			return
		}

		ts := s.Now()
		if len(samples) > 0 && s.Store != nil {
			if err := s.Store(samples, ts); err != nil {
				slog.Error("metrics store failed", "err", err)
			}
		}
		if s.OpsStore != nil {
			if err := s.OpsStore(snapshot, ts); err != nil {
				slog.Error("ops metrics store failed", "err", err)
			}
		}
	}

	// Fire immediately on first iteration.
	tick()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			tick()
		}
	}
}

// DBStoreFn returns a StoreFn that writes samples to traffic_samples,
// skipping rows with duplicate (user_label, ts). Teleproxy exposes cumulative
// counters, so this stores per-scrape deltas for traffic reports and quotas.
func DBStoreFn(database *db.DB) StoreFn {
	return func(samples []Sample, ts int64) error {
		if len(samples) == 0 {
			return nil
		}

		tx, err := database.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback() //nolint:errcheck

		sampleStmt, err := tx.Prepare(`
			INSERT INTO traffic_samples(user_label, ts, bytes_in, bytes_out, connections)
			SELECT ?, ?, ?, ?, ?
			WHERE NOT EXISTS (
				SELECT 1 FROM traffic_samples WHERE user_label=? AND ts=?
			)
		`)
		if err != nil {
			return err
		}
		defer sampleStmt.Close()

		dailyStmt, err := tx.Prepare(`
			INSERT INTO traffic_daily(user_label, day_ts, bytes_in, bytes_out, connections)
			VALUES(?, ?, ?, ?, ?)
			ON CONFLICT(user_label, day_ts) DO UPDATE SET
			    bytes_in = bytes_in + excluded.bytes_in,
			    bytes_out = bytes_out + excluded.bytes_out,
			    connections = MAX(connections, excluded.connections)
		`)
		if err != nil {
			return err
		}
		defer dailyStmt.Close()

		counterStmt, err := tx.Prepare(`
			INSERT INTO traffic_counters(user_label, bytes_in, bytes_out, updated_ts)
			VALUES(?, ?, ?, ?)
			ON CONFLICT(user_label) DO UPDATE SET
			    bytes_in = excluded.bytes_in,
			    bytes_out = excluded.bytes_out,
			    updated_ts = excluded.updated_ts
		`)
		if err != nil {
			return err
		}
		defer counterStmt.Close()

		for _, sample := range samples {
			deltaIn, deltaOut, err := counterDelta(tx, sample)
			if err != nil {
				return err
			}
			res, err := sampleStmt.Exec(
				sample.UserLabel, ts, deltaIn, deltaOut, sample.Connections,
				sample.UserLabel, ts,
			)
			if err != nil {
				return err
			}
			inserted, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if inserted == 0 {
				continue
			}
			dayTS := ts - (ts % 86400)
			if _, err := dailyStmt.Exec(sample.UserLabel, dayTS, deltaIn, deltaOut, sample.Connections); err != nil {
				return err
			}
			if _, err := counterStmt.Exec(sample.UserLabel, sample.BytesIn, sample.BytesOut, ts); err != nil {
				return err
			}
		}

		return tx.Commit()
	}
}

func counterDelta(tx interface {
	QueryRow(query string, args ...any) *sql.Row
}, sample Sample) (int64, int64, error) {
	var prevIn, prevOut int64
	err := tx.QueryRow(
		`SELECT bytes_in, bytes_out FROM traffic_counters WHERE user_label=?`,
		sample.UserLabel,
	).Scan(&prevIn, &prevOut)
	if err != nil && err != sql.ErrNoRows {
		return 0, 0, err
	}
	if err == sql.ErrNoRows {
		return sample.BytesIn, sample.BytesOut, nil
	}
	deltaIn := sample.BytesIn
	if sample.BytesIn >= prevIn {
		deltaIn = sample.BytesIn - prevIn
	}
	deltaOut := sample.BytesOut
	if sample.BytesOut >= prevOut {
		deltaOut = sample.BytesOut - prevOut
	}
	return deltaIn, deltaOut, nil
}
