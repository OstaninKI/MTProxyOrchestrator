package panel_test

import (
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
)

func TestRateLimiterAllowsUnderThreshold(t *testing.T) {
	now := time.Now()
	rl := panel.NewRateLimiterWithClock(func() time.Time { return now })
	for range 4 {
		rl.RecordFailure("1.2.3.4")
	}
	if rl.IsBlocked("1.2.3.4") {
		t.Error("should not be blocked before 5 failures")
	}
}

func TestRateLimiterBlocksAtThreshold(t *testing.T) {
	now := time.Now()
	rl := panel.NewRateLimiterWithClock(func() time.Time { return now })
	for range 5 {
		rl.RecordFailure("1.2.3.4")
	}
	if !rl.IsBlocked("1.2.3.4") {
		t.Error("should be blocked after 5 failures")
	}
}

func TestRateLimiterBlockExpires(t *testing.T) {
	var current time.Time
	rl := panel.NewRateLimiterWithClock(func() time.Time { return current })
	current = time.Now()
	for range 5 {
		rl.RecordFailure("1.2.3.4")
	}
	current = current.Add(time.Hour + time.Second)
	if rl.IsBlocked("1.2.3.4") {
		t.Error("block should have expired")
	}
}

func TestRateLimiterWindowReset(t *testing.T) {
	var current time.Time
	rl := panel.NewRateLimiterWithClock(func() time.Time { return current })
	current = time.Now()
	for range 4 {
		rl.RecordFailure("1.2.3.4")
	}
	current = current.Add(5*time.Minute + time.Second)
	for range 4 {
		rl.RecordFailure("1.2.3.4")
	}
	if rl.IsBlocked("1.2.3.4") {
		t.Error("should not be blocked after window reset")
	}
}

func TestRateLimiterSuccessResetsCount(t *testing.T) {
	now := time.Now()
	rl := panel.NewRateLimiterWithClock(func() time.Time { return now })
	for range 4 {
		rl.RecordFailure("1.2.3.4")
	}
	rl.RecordSuccess("1.2.3.4")
	for range 4 {
		rl.RecordFailure("1.2.3.4")
	}
	if rl.IsBlocked("1.2.3.4") {
		t.Error("should not be blocked after success reset")
	}
}

func TestRateLimiterUnknownIP(t *testing.T) {
	rl := panel.NewRateLimiter()
	if rl.IsBlocked("9.9.9.9") {
		t.Error("unknown IP should not be blocked")
	}
}
