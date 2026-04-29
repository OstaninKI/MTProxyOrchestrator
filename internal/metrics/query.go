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
