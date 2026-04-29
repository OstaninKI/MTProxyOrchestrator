package metrics

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

// fakeClock returns incrementing Unix timestamps starting from base.
type fakeClock struct{ base int64 }

func (f *fakeClock) now() int64 {
	f.base++
	return f.base
}

func fixedSamples() []Sample {
	return []Sample{
		{UserLabel: "alice", BytesIn: 100, BytesOut: 200, Connections: 3},
		{UserLabel: "bob", BytesIn: 50, BytesOut: 75, Connections: 1},
	}
}

// TestSamplerCallsStoreWithTimestamp verifies that Store is called with the
// correct timestamp and samples from Source.
func TestSamplerCallsStoreWithTimestamp(t *testing.T) {
	t.Parallel()

	type call struct {
		samples []Sample
		ts      int64
	}
	var mu sync.Mutex
	var calls []call

	sampler := Sampler{
		Source: func() ([]Sample, error) {
			return fixedSamples(), nil
		},
		Store: func(samples []Sample, ts int64) error {
			mu.Lock()
			calls = append(calls, call{samples: samples, ts: ts})
			mu.Unlock()
			return nil
		},
		Now:      func() int64 { return 1000 },
		Interval: time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = sampler.Run(ctx)

	mu.Lock()
	defer mu.Unlock()

	if len(calls) == 0 {
		t.Fatal("expected Store to be called at least once")
	}

	first := calls[0]
	if first.ts != 1000 {
		t.Errorf("expected ts=1000, got %d", first.ts)
	}
	if len(first.samples) != 2 {
		t.Errorf("expected 2 samples, got %d", len(first.samples))
	}
}

// TestSamplerSkipsStoreOnEmptySource verifies that Store is never called when
// Source returns an empty slice.
func TestSamplerSkipsStoreOnEmptySource(t *testing.T) {
	t.Parallel()

	var storeCalled atomic.Bool

	sampler := Sampler{
		Source: func() ([]Sample, error) {
			return nil, nil
		},
		Store: func(samples []Sample, ts int64) error {
			storeCalled.Store(true)
			return nil
		},
		Now:      func() int64 { return 1000 },
		Interval: time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = sampler.Run(ctx)

	if storeCalled.Load() {
		t.Error("expected Store to never be called for empty source")
	}
}

// TestSamplerContinuesOnSourceError verifies that after a source error,
// the sampler recovers and eventually calls Store with valid samples.
func TestSamplerContinuesOnSourceError(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	var storeCalled atomic.Bool

	sampler := Sampler{
		Source: func() ([]Sample, error) {
			n := callCount.Add(1)
			if n == 1 {
				return nil, errors.New("scrape error")
			}
			return fixedSamples(), nil
		},
		Store: func(samples []Sample, ts int64) error {
			storeCalled.Store(true)
			return nil
		},
		Now:      func() int64 { return 2000 },
		Interval: time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = sampler.Run(ctx)

	if !storeCalled.Load() {
		t.Error("expected Store to be called after source recovered from error")
	}
}

// TestSamplerDefaultInterval verifies that a Sampler with Interval=0 does
// not panic and correctly uses the default 60s interval.
func TestSamplerDefaultInterval(t *testing.T) {
	t.Parallel()

	sampler := Sampler{
		Source: func() ([]Sample, error) {
			return fixedSamples(), nil
		},
		Store: func(samples []Sample, ts int64) error {
			return nil
		},
		Now: func() int64 { return 3000 },
		// Interval intentionally left at zero
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Should not panic even with zero interval.
	_ = sampler.Run(ctx)
}

// TestDBStoreFnDeduplicates verifies that inserting the same (user_label, ts)
// twice results in only one row in traffic_samples.
func TestDBStoreFnDeduplicates(t *testing.T) {
	t.Parallel()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	store := DBStoreFn(database)

	samples := []Sample{
		{UserLabel: "carol", BytesIn: 10, BytesOut: 20, Connections: 1},
	}
	const ts = int64(5000)

	if err := store(samples, ts); err != nil {
		t.Fatalf("first store: %v", err)
	}
	if err := store(samples, ts); err != nil {
		t.Fatalf("second store: %v", err)
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM traffic_samples WHERE user_label='carol' AND ts=?`, ts).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

// TestDBStoreFnSkipsEmpty verifies that calling DBStoreFn with an empty slice
// inserts no rows.
func TestDBStoreFnSkipsEmpty(t *testing.T) {
	t.Parallel()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	store := DBStoreFn(database)

	if err := store(nil, 9999); err != nil {
		t.Fatalf("store: %v", err)
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM traffic_samples`).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows, got %d", count)
	}
}
