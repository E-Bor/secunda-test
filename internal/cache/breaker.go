package cache

import (
	"sync"
	"time"
)

type breaker struct {
	mu         sync.Mutex
	failures   int
	retryAt    time.Time
	probe      bool
	generation uint64
	threshold  int
	cooldown   time.Duration
}

func (b *breaker) allow(now time.Time) (uint64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.retryAt.IsZero() {
		return b.generation, true
	}
	if now.Before(b.retryAt) || b.probe {
		return 0, false
	}

	b.probe = true
	return b.generation, true
}

func (b *breaker) finish(generation uint64, success bool, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if generation != b.generation {
		return false
	}

	if success {
		b.failures = 0
		if b.probe {
			b.probe = false
			b.retryAt = time.Time{}
			b.generation++
		}
		return false
	}

	b.failures++
	if !b.probe && b.failures < b.threshold {
		return false
	}

	b.retryAt = now.Add(b.cooldown)
	b.probe = false
	b.generation++
	return true
}
