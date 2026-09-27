package database

import (
	"context"
	"fmt"

	"github.com/migus/url_shortener/internal/config"
	"github.com/redis/go-redis/v9"
)

// NewRedisClient opens a Redis client and verifies connectivity.
func NewRedisClient(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return rdb, nil
}
