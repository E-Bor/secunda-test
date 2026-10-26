package cache

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBreaker(t *testing.T) {
	now := time.Unix(100, 0)
	b := breaker{threshold: 2, cooldown: time.Second}
	first, ok := b.allow(now)
	if !ok {
		t.Fatal("closed breaker denied request")
	}
	b.finish(first, false, now)
	second, ok := b.allow(now)
	if !ok {
		t.Fatal("breaker opened too early")
	}
	if !b.finish(second, false, now) {
		t.Fatal("breaker did not open")
	}
	b.finish(first, true, now)
	if _, ok := b.allow(now); ok {
		t.Fatal("late success closed an open breaker")
	}
	if _, ok := b.allow(now.Add(999 * time.Millisecond)); ok {
		t.Fatal("cooldown ignored")
	}
	var probes atomic.Int32
	var generation uint64
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			g, ok := b.allow(now.Add(time.Second))
			if ok {
				probes.Add(1)
				generation = g
			}
		})
	}
	wg.Wait()
	if probes.Load() != 1 {
		t.Fatalf("probes = %d, want 1", probes.Load())
	}
	if !b.finish(generation, false, now.Add(time.Second)) {
		t.Fatal("failed probe must reopen breaker")
	}
	if _, ok := b.allow(now.Add(time.Second)); ok {
		t.Fatal("failed probe did not delay retry")
	}
	generation, ok = b.allow(now.Add(2 * time.Second))
	if !ok {
		t.Fatal("probe denied after cooldown")
	}
	b.finish(generation, true, now.Add(2*time.Second))
	for range 3 {
		if _, ok := b.allow(now.Add(2 * time.Second)); !ok {
			t.Fatal("successful probe did not close breaker")
		}
	}
}
