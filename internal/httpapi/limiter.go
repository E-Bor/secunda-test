package httpapi

import (
	"sync"
	"time"
)

type bucket struct {
	tokens  float64
	updated time.Time
}

type limiter struct {
	mu        sync.Mutex
	buckets   map[string]bucket
	limit     int
	lastSweep time.Time
}

func newLimiter(limit int) *limiter {
	return &limiter{buckets: make(map[string]bucket), limit: limit}
}

func (l *limiter) allow(key string, now time.Time) bool {
	if l.limit <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now)
	b, exists := l.buckets[key]
	if !exists {
		if len(l.buckets) >= 10000 {
			return false
		}
		b = bucket{tokens: float64(l.limit), updated: now}
	}

	if now.Before(b.updated) {
		now = b.updated
	}
	b.tokens = min(float64(l.limit), b.tokens+now.Sub(b.updated).Seconds()*float64(l.limit)/60)
	b.updated = now

	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	l.buckets[key] = b

	return allowed
}

func (l *limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}

	for key, b := range l.buckets {
		if now.Sub(b.updated) >= 2*time.Minute {
			delete(l.buckets, key)
		}
	}
	l.lastSweep = now
}
