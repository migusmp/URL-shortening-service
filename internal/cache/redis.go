package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "url:"

type URLCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewURLCache(client *redis.Client, ttl time.Duration) *URLCache {
	return &URLCache{client: client, ttl: ttl}
}

func (c *URLCache) Get(ctx context.Context, shortCode string) (string, bool, error) {
	val, err := c.client.Get(ctx, keyPrefix+shortCode).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("cache get: %w", err)
	}
	return val, true, nil
}

func (c *URLCache) Set(ctx context.Context, shortCode, longURL string) error {
	if err := c.client.Set(ctx, keyPrefix+shortCode, longURL, c.ttl).Err(); err != nil {
		return fmt.Errorf("cache set: %w", err)
	}
	return nil
}

func (c *URLCache) Delete(ctx context.Context, shortCode string) error {
	if err := c.client.Del(ctx, keyPrefix+shortCode).Err(); err != nil {
		return fmt.Errorf("cache delete: %w", err)
	}
	return nil
}

// RateLimiter implements a fixed-window counter using Redis INCR + EXPIRE.
type RateLimiter struct {
	client *redis.Client
	window time.Duration
}

func NewRateLimiter(client *redis.Client, window time.Duration) *RateLimiter {
	return &RateLimiter{client: client, window: window}
}

// Allow increments the counter for key and returns whether the request is within limit.
func (r *RateLimiter) Allow(ctx context.Context, key string, limit int) (bool, error) {
	redisKey := "rl:" + key

	pipe := r.client.TxPipeline()
	incr := pipe.Incr(ctx, redisKey)
	pipe.Expire(ctx, redisKey, r.window)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("rate limit: %w", err)
	}

	count := incr.Val()
	return count <= int64(limit), nil
}
