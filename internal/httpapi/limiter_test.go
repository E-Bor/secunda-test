package httpapi

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := newLimiter(2)
	now := time.Unix(100, 0)
	if !l.allow("a", now) || !l.allow("a", now) || l.allow("a", now) {
		t.Fatal("burst limit failed")
	}
	if !l.allow("b", now) {
		t.Fatal("independent clients share a bucket")
	}
	if l.allow("a", now.Add(29*time.Second)) {
		t.Fatal("tokens refilled too soon")
	}
	if !l.allow("a", now.Add(30*time.Second)) {
		t.Fatal("tokens were not refilled")
	}
	l.allow("c", now.Add(3*time.Minute))
	if len(l.buckets) != 1 {
		t.Fatalf("expired buckets remain: %d", len(l.buckets))
	}
}

func TestLimiterConcurrent(t *testing.T) {
	l := newLimiter(10)
	now := time.Now()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if l.allow("same-user", now) {
				accepted.Add(1)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 10 {
		t.Fatalf("accepted %d requests", accepted.Load())
	}
}

func TestLimiterCapacity(t *testing.T) {
	l := newLimiter(1)
	now := time.Now()
	for i := range 10000 {
		l.allow(fmt.Sprint(i), now)
	}
	if l.allow("overflow", now) {
		t.Fatal("bucket limit ignored")
	}
	if !l.allow("overflow", now.Add(3*time.Minute)) {
		t.Fatal("stale buckets not collected")
	}
}
