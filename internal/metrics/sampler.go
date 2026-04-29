package metrics

import (
	"context"
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
type Sampler struct {
	Source   ScrapeSource
	Store    StoreFn
	Now      NowFn
	Interval time.Duration // default 60s when zero
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
		samples, err := s.Source()
		if err != nil {
			slog.Error("metrics scrape failed", "err", err)
			return
		}
		if len(samples) == 0 {
			return
		}
		ts := s.Now()
		if err := s.Store(samples, ts); err != nil {
			slog.Error("metrics store failed", "err", err)
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
// skipping rows with duplicate (user_label, ts).
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

		stmt, err := tx.Prepare(`
			INSERT INTO traffic_samples(user_label, ts, bytes_in, bytes_out, connections)
			SELECT ?, ?, ?, ?, ?
			WHERE NOT EXISTS (
				SELECT 1 FROM traffic_samples WHERE user_label=? AND ts=?
			)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, sample := range samples {
			if _, err := stmt.Exec(
				sample.UserLabel, ts, sample.BytesIn, sample.BytesOut, sample.Connections,
				sample.UserLabel, ts,
			); err != nil {
				return err
			}
		}

		return tx.Commit()
	}
}
