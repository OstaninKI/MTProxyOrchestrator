package metrics

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

// SnapshotSource returns the full Teleproxy metrics snapshot for one scrape.
// Matches Scraper.ScrapeSnapshot for easy injection.
type SnapshotSource func() (Snapshot, error)

// OpsStoreFn persists a snapshot's global operational counters.
type OpsStoreFn func(snapshot Snapshot, ts int64) error

// OpsBucket holds aggregated operational counts for one dashboard chart point.
// Values are per-interval deltas summed within the bucket, not cumulative totals.
type OpsBucket struct {
	TS              int64
	Accepted        int64
	Rejected        int64
	SOCKS5Attempted int64
	SOCKS5Succeeded int64
}

type opsCounters struct {
	accepted        int64
	rejected        int64
	socks5Attempted int64
	socks5Succeeded int64
}

// DBOpsStoreFn returns an OpsStoreFn that records per-interval deltas of the
// Teleproxy global counters into ops_samples, keeping the last cumulative
// values in ops_counters. The first scrape (no prior counters) only seeds
// ops_counters so the trend does not start with a startup spike.
func DBOpsStoreFn(database *db.DB) OpsStoreFn {
	return func(snapshot Snapshot, ts int64) error {
		cur := opsCounters{
			accepted:        snapshot.AcceptedConnectionsTotal,
			rejected:        snapshot.RejectedConnectionsTotal,
			socks5Attempted: snapshot.SOCKS5.Attempted,
			socks5Succeeded: snapshot.SOCKS5.Succeeded,
		}

		tx, err := database.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback() //nolint:errcheck

		var prev opsCounters
		err = tx.QueryRow(
			`SELECT accepted, rejected, socks5_attempted, socks5_succeeded FROM ops_counters WHERE id=1`,
		).Scan(&prev.accepted, &prev.rejected, &prev.socks5Attempted, &prev.socks5Succeeded)
		hasPrev := true
		if err == sql.ErrNoRows {
			hasPrev = false
		} else if err != nil {
			return err
		}

		if hasPrev {
			// Skip duplicate timestamps so repeated scrapes within one second
			// do not create double rows.
			_, err = tx.Exec(`
				INSERT INTO ops_samples(ts, accepted, rejected, socks5_attempted, socks5_succeeded)
				SELECT ?, ?, ?, ?, ?
				WHERE NOT EXISTS (SELECT 1 FROM ops_samples WHERE ts=?)
			`,
				ts,
				counterDeltaInt(cur.accepted, prev.accepted),
				counterDeltaInt(cur.rejected, prev.rejected),
				counterDeltaInt(cur.socks5Attempted, prev.socks5Attempted),
				counterDeltaInt(cur.socks5Succeeded, prev.socks5Succeeded),
				ts,
			)
			if err != nil {
				return err
			}
		}

		_, err = tx.Exec(`
			INSERT INTO ops_counters(id, accepted, rejected, socks5_attempted, socks5_succeeded, updated_ts)
			VALUES(1, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
			    accepted = excluded.accepted,
			    rejected = excluded.rejected,
			    socks5_attempted = excluded.socks5_attempted,
			    socks5_succeeded = excluded.socks5_succeeded,
			    updated_ts = excluded.updated_ts
		`, cur.accepted, cur.rejected, cur.socks5Attempted, cur.socks5Succeeded, ts)
		if err != nil {
			return err
		}

		return tx.Commit()
	}
}

// counterDeltaInt returns cur-prev, or cur when the counter appears to have
// reset (cur < prev), matching the per-user traffic delta convention.
func counterDeltaInt(cur, prev int64) int64 {
	if cur >= prev {
		return cur - prev
	}
	return cur
}

// QueryOpsSeries returns exactly buckets aggregated operational points for the
// period. Missing buckets are zero-filled so the dashboard renders a stable
// sparkline. ops_samples is retained at minute granularity, so windows longer
// than the retention horizon return only the data that exists.
func QueryOpsSeries(d *db.DB, p Period, buckets int, now func() int64) ([]OpsBucket, error) {
	if buckets <= 0 {
		return nil, fmt.Errorf("invalid bucket count %d", buckets)
	}
	if now == nil {
		now = func() int64 { return time.Now().Unix() }
	}

	duration := int64(p.Duration().Seconds())
	if duration <= 0 {
		duration = int64((24 * time.Hour).Seconds())
	}
	step := duration / int64(buckets)
	if step < 1 {
		step = 1
	}

	end := now()
	start := end - duration
	series := make([]OpsBucket, buckets)
	for i := range series {
		series[i].TS = start + int64(i)*step
	}

	rows, err := d.Query(`
SELECT ts, accepted, rejected, socks5_attempted, socks5_succeeded
FROM ops_samples
WHERE ts >= ?
ORDER BY ts ASC`, start)
	if err != nil {
		return nil, fmt.Errorf("query ops series: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var b OpsBucket
		if err := rows.Scan(&b.TS, &b.Accepted, &b.Rejected, &b.SOCKS5Attempted, &b.SOCKS5Succeeded); err != nil {
			return nil, fmt.Errorf("scan ops series: %w", err)
		}
		if b.TS < start || b.TS >= end {
			continue
		}
		idx := int((b.TS - start) / step)
		if idx < 0 || idx >= len(series) {
			continue
		}
		series[idx].Accepted += b.Accepted
		series[idx].Rejected += b.Rejected
		series[idx].SOCKS5Attempted += b.SOCKS5Attempted
		series[idx].SOCKS5Succeeded += b.SOCKS5Succeeded
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ops series: %w", err)
	}

	return series, nil
}
