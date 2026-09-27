package cache_test

import (
	"context"
	"sync"
	"testing"
	"time"
)

// inMemoryLimiter mirrors the fixed-window Allow semantics for unit tests
// without requiring a live Redis instance.
type inMemoryLimiter struct {
	mu      sync.Mutex
	counts  map[string]int64
	expires map[string]time.Time
	window  time.Duration
}

func newInMemoryLimiter(window time.Duration) *inMemoryLimiter {
	return &inMemoryLimiter{
		counts:  make(map[string]int64),
		expires: make(map[string]time.Time),
		window:  window,
	}
}

func (l *inMemoryLimiter) Allow(_ context.Context, key string, limit int) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if exp, ok := l.expires[key]; !ok || now.After(exp) {
		l.counts[key] = 0
		l.expires[key] = now.Add(l.window)
	}
	l.counts[key]++
	return l.counts[key] <= int64(limit), nil
}

func TestRateLimiterAllowsWithinLimit(t *testing.T) {
	lim := newInMemoryLimiter(time.Minute)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		ok, err := lim.Allow(ctx, "ip:1", 3)
		if err != nil {
			t.Fatalf("allow: %v", err)
		}
		if !ok {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	ok, err := lim.Allow(ctx, "ip:1", 3)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if ok {
		t.Fatalf("4th request should be denied")
	}
}

func TestRateLimiterIsolatesKeys(t *testing.T) {
	lim := newInMemoryLimiter(time.Minute)
	ctx := context.Background()

	ok, err := lim.Allow(ctx, "a", 1)
	if err != nil || !ok {
		t.Fatalf("first key should allow")
	}
	ok, err = lim.Allow(ctx, "b", 1)
	if err != nil || !ok {
		t.Fatalf("second key should have its own budget")
	}
}
