package panel

import (
	"sync"
	"time"
)

const (
	maxFailures   = 5
	failureWindow = 5 * time.Minute
	blockDuration = time.Hour
)

type ipRecord struct {
	failures     int
	windowEnd    time.Time
	blockedUntil time.Time
}

// RateLimiter tracks failed login attempts by IP.
type RateLimiter struct {
	mu      sync.Mutex
	records map[string]*ipRecord
	now     func() time.Time
}

// NewRateLimiter creates a limiter with the real clock.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{records: make(map[string]*ipRecord), now: time.Now}
}

// NewRateLimiterWithClock creates a limiter with an injectable clock.
func NewRateLimiterWithClock(clock func() time.Time) *RateLimiter {
	return &RateLimiter{records: make(map[string]*ipRecord), now: clock}
}

// IsBlocked returns true when the IP is currently blocked.
func (rl *RateLimiter) IsBlocked(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rec := rl.records[ip]
	if rec == nil {
		return false
	}
	return rl.now().Before(rec.blockedUntil)
}

// RecordFailure records one failed login attempt for ip.
func (rl *RateLimiter) RecordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	rec := rl.records[ip]
	if rec == nil {
		rec = &ipRecord{}
		rl.records[ip] = rec
	}
	if now.After(rec.windowEnd) {
		rec.failures = 0
		rec.windowEnd = now.Add(failureWindow)
	}
	rec.failures++
	if rec.failures >= maxFailures {
		rec.blockedUntil = now.Add(blockDuration)
	}
}

// RecordSuccess resets the failure counter for ip.
func (rl *RateLimiter) RecordSuccess(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.records, ip)
}
