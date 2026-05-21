package metrics

import (
	"fmt"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

// Period represents a dashboard time window.
type Period string

const (
	Period1h  Period = "1h"
	Period24h Period = "24h"
	Period7d  Period = "7d"
	Period30d Period = "30d"
)

// ParsePeriod converts a string to a Period, defaulting to Period24h on unknown values.
func ParsePeriod(s string) Period {
	switch Period(s) {
	case Period1h, Period24h, Period7d, Period30d:
		return Period(s)
	default:
		return Period24h
	}
}

// Duration returns the time.Duration for the period.
func (p Period) Duration() time.Duration {
	switch p {
	case Period1h:
		return time.Hour
	case Period24h:
		return 24 * time.Hour
	case Period7d:
		return 7 * 24 * time.Hour
	case Period30d:
		return 30 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// UserTraffic holds aggregated traffic stats for one user.
type UserTraffic struct {
	UserLabel   string
	BytesIn     int64
	BytesOut    int64
	Connections int64
}

// TrafficBucket holds an aggregated point for dashboard charts.
type TrafficBucket struct {
	TS          int64
	BytesIn     int64
	BytesOut    int64
	Connections int64
}

// QueryTopUsers returns up to n users ordered by total traffic (bytes_in + bytes_out)
// for the given period. Queries traffic_samples for recent data and traffic_hourly
// for older data depending on the window.
func QueryTopUsers(d *db.DB, p Period, n int, now func() int64) ([]UserTraffic, error) {
	if now == nil {
		now = func() int64 { return time.Now().Unix() }
	}
	cutoff := now() - int64(p.Duration().Seconds())

	// For periods <= 7 days, traffic_samples holds the data.
	// For longer periods, also include traffic_hourly.
	var rows []UserTraffic
	var err error

	if p == Period30d {
		rows, err = queryTopFromBoth(d, cutoff, n)
	} else {
		rows, err = queryTopFromSamples(d, cutoff, n)
	}
	return rows, err
}

// QueryTrafficSeries returns exactly buckets aggregated points for the period.
// Missing buckets are zero-filled so the dashboard can render stable charts.
func QueryTrafficSeries(d *db.DB, p Period, buckets int, now func() int64) ([]TrafficBucket, error) {
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
	series := make([]TrafficBucket, buckets)
	for i := range series {
		series[i].TS = start + int64(i)*step
	}

	rows, err := queryTrafficRows(d, p, start)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.TS < start || row.TS >= end {
			continue
		}
		idx := int((row.TS - start) / step)
		if idx < 0 || idx >= len(series) {
			continue
		}
		series[idx].BytesIn += row.BytesIn
		series[idx].BytesOut += row.BytesOut
		if row.Connections > series[idx].Connections {
			series[idx].Connections = row.Connections
		}
	}

	return series, nil
}

// queryTopFromSamples queries only traffic_samples for the top n users.
func queryTopFromSamples(d *db.DB, cutoff int64, n int) ([]UserTraffic, error) {
	const q = `
SELECT user_label,
       SUM(bytes_in)    AS total_in,
       SUM(bytes_out)   AS total_out,
       MAX(connections) AS max_conn
FROM traffic_samples
WHERE ts >= ?
GROUP BY user_label
ORDER BY (SUM(bytes_in) + SUM(bytes_out)) DESC
LIMIT ?`

	return scanUserTraffic(d, q, cutoff, n)
}

// queryTopFromBoth combines traffic_samples and traffic_hourly for a 30d window.
func queryTopFromBoth(d *db.DB, cutoff int64, n int) ([]UserTraffic, error) {
	const q = `
SELECT user_label,
       SUM(bytes_in)    AS total_in,
       SUM(bytes_out)   AS total_out,
       MAX(connections) AS max_conn
FROM (
    SELECT user_label, bytes_in, bytes_out, connections FROM traffic_samples WHERE ts >= ?
    UNION ALL
    SELECT user_label, bytes_in, bytes_out, connections FROM traffic_hourly WHERE hour_ts >= ?
)
GROUP BY user_label
ORDER BY (SUM(bytes_in) + SUM(bytes_out)) DESC
LIMIT ?`

	sqlRows, err := d.Query(q, cutoff, cutoff, n)
	if err != nil {
		return nil, fmt.Errorf("query top users: %w", err)
	}
	defer sqlRows.Close()
	return scanRows(sqlRows)
}

type trafficRow struct {
	TS          int64
	BytesIn     int64
	BytesOut    int64
	Connections int64
}

func queryTrafficRows(d *db.DB, p Period, cutoff int64) ([]trafficRow, error) {
	query := `
SELECT ts, SUM(bytes_in), SUM(bytes_out), MAX(connections)
FROM traffic_samples
WHERE ts >= ?
GROUP BY ts
ORDER BY ts ASC`
	args := []any{cutoff}

	if p == Period30d {
		query = `
SELECT ts, SUM(bytes_in), SUM(bytes_out), MAX(connections)
FROM (
    SELECT ts, bytes_in, bytes_out, connections
    FROM traffic_samples
    WHERE ts >= ?
    UNION ALL
    SELECT hour_ts AS ts, bytes_in, bytes_out, connections
    FROM traffic_hourly
    WHERE hour_ts >= ?
)
GROUP BY ts
ORDER BY ts ASC`
		args = []any{cutoff, cutoff}
	}

	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query traffic series: %w", err)
	}
	defer rows.Close()

	var out []trafficRow
	for rows.Next() {
		var row trafficRow
		if err := rows.Scan(&row.TS, &row.BytesIn, &row.BytesOut, &row.Connections); err != nil {
			return nil, fmt.Errorf("scan traffic series: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate traffic series: %w", err)
	}
	return out, nil
}

func scanUserTraffic(d *db.DB, q string, args ...any) ([]UserTraffic, error) {
	sqlRows, err := d.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query traffic: %w", err)
	}
	defer sqlRows.Close()
	return scanRows(sqlRows)
}

// scanRows scans SQL rows into []UserTraffic.
func scanRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]UserTraffic, error) {
	var out []UserTraffic
	for rows.Next() {
		var u UserTraffic
		if err := rows.Scan(&u.UserLabel, &u.BytesIn, &u.BytesOut, &u.Connections); err != nil {
			return nil, fmt.Errorf("scan user traffic: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
