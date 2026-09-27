package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/migus/url_shortener/internal/cache"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return mr, client
}

func TestURLCacheSetGetDelete(t *testing.T) {
	_, client := newTestRedis(t)
	c := cache.NewURLCache(client, time.Hour)
	ctx := context.Background()

	got, hit, err := c.Get(ctx, "abc")
	if err != nil || hit || got != "" {
		t.Fatalf("miss expected, got=%q hit=%v err=%v", got, hit, err)
	}

	if err := c.Set(ctx, "abc", "https://example.com"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, hit, err = c.Get(ctx, "abc")
	if err != nil || !hit || got != "https://example.com" {
		t.Fatalf("hit expected, got=%q hit=%v err=%v", got, hit, err)
	}

	if err := c.Delete(ctx, "abc"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, hit, err = c.Get(ctx, "abc")
	if err != nil || hit || got != "" {
		t.Fatalf("after delete miss expected, got=%q hit=%v err=%v", got, hit, err)
	}
}

func TestURLCacheTTL(t *testing.T) {
	mr, client := newTestRedis(t)
	c := cache.NewURLCache(client, time.Second)
	ctx := context.Background()

	if err := c.Set(ctx, "ttl", "https://example.com"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	mr.FastForward(2 * time.Second)

	_, hit, err := c.Get(ctx, "ttl")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Fatal("expected miss after TTL")
	}
}

func TestURLCacheGetClosedClient(t *testing.T) {
	_, client := newTestRedis(t)
	c := cache.NewURLCache(client, time.Hour)
	_ = client.Close()

	_, _, err := c.Get(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error from closed client")
	}
}

func TestRateLimiterAllowAndDeny(t *testing.T) {
	_, client := newTestRedis(t)
	lim := cache.NewRateLimiter(client, time.Minute)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		ok, err := lim.Allow(ctx, "ip:1", 3)
		if err != nil {
			t.Fatalf("Allow: %v", err)
		}
		if !ok {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	ok, err := lim.Allow(ctx, "ip:1", 3)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if ok {
		t.Fatal("4th request should be denied")
	}
}

func TestRateLimiterIsolatesKeys(t *testing.T) {
	_, client := newTestRedis(t)
	lim := cache.NewRateLimiter(client, time.Minute)
	ctx := context.Background()

	ok, err := lim.Allow(ctx, "a", 1)
	if err != nil || !ok {
		t.Fatal("first key should allow")
	}
	ok, err = lim.Allow(ctx, "b", 1)
	if err != nil || !ok {
		t.Fatal("second key should have its own budget")
	}
}

func TestRateLimiterWindowReset(t *testing.T) {
	mr, client := newTestRedis(t)
	lim := cache.NewRateLimiter(client, time.Second)
	ctx := context.Background()

	ok, err := lim.Allow(ctx, "w", 1)
	if err != nil || !ok {
		t.Fatal("first should allow")
	}
	ok, err = lim.Allow(ctx, "w", 1)
	if err != nil || ok {
		t.Fatal("second should deny")
	}

	mr.FastForward(2 * time.Second)

	ok, err = lim.Allow(ctx, "w", 1)
	if err != nil || !ok {
		t.Fatal("after window reset should allow")
	}
}
