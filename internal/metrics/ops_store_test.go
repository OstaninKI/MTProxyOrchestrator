package metrics_test

import (
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/metrics"
)

func TestDBOpsStoreFnRecordsDeltasAndQuerySeries(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	defer d.Close()

	store := metrics.DBOpsStoreFn(d)

	// First scrape only seeds counters (no startup spike).
	snap := func(acc, rej, att, succ int64) metrics.Snapshot {
		return metrics.Snapshot{
			AcceptedConnectionsTotal: acc,
			RejectedConnectionsTotal: rej,
			SOCKS5:                   metrics.UpstreamCounters{Attempted: att, Succeeded: succ},
		}
	}

	now := int64(1_000_000)
	if err := store(snap(100, 10, 50, 48), now); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	// Second scrape: deltas accepted=20, rejected=5, attempted=10, succeeded=9.
	if err := store(snap(120, 15, 60, 57), now+60); err != nil {
		t.Fatalf("second store: %v", err)
	}
	// Third scrape with a counter reset (accepted dropped): delta = current value.
	if err := store(snap(5, 16, 61, 58), now+120); err != nil {
		t.Fatalf("third store: %v", err)
	}

	series, err := metrics.QueryOpsSeries(d, metrics.Period1h, 60, func() int64 { return now + 130 })
	if err != nil {
		t.Fatalf("query ops series: %v", err)
	}

	var totAcc, totRej, totAtt, totSucc int64
	for _, b := range series {
		totAcc += b.Accepted
		totRej += b.Rejected
		totAtt += b.SOCKS5Attempted
		totSucc += b.SOCKS5Succeeded
	}
	// Two recorded rows: (20,5,10,9) and (5,1,1,1).
	if totAcc != 25 || totRej != 6 || totAtt != 11 || totSucc != 10 {
		t.Fatalf("series totals = acc:%d rej:%d att:%d succ:%d, want 25/6/11/10", totAcc, totRej, totAtt, totSucc)
	}
}

func TestRetainerDeleteOldOps(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	defer d.Close()

	store := metrics.DBOpsStoreFn(d)
	if err := store(metrics.Snapshot{AcceptedConnectionsTotal: 1}, 1000); err != nil {
		t.Fatal(err)
	}
	if err := store(metrics.Snapshot{AcceptedConnectionsTotal: 2}, 2000); err != nil {
		t.Fatal(err)
	}
	if err := store(metrics.Snapshot{AcceptedConnectionsTotal: 3}, 9000); err != nil {
		t.Fatal(err)
	}

	r := metrics.Retainer{DB: d}
	if err := r.DeleteOldOps(5000); err != nil {
		t.Fatalf("delete old ops: %v", err)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM ops_samples`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	// Row at ts=2000 (delta from 1000) deleted; row at ts=9000 kept.
	if count != 1 {
		t.Fatalf("ops_samples count = %d, want 1", count)
	}
}
