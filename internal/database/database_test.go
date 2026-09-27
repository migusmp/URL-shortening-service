package database_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/migus/url_shortener/internal/config"
	"github.com/migus/url_shortener/internal/database"
)

func TestNewPostgresPoolSuccess(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL / DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := database.NewPostgresPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPostgresPool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestNewPostgresPoolInvalidDSN(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := database.NewPostgresPool(ctx, "postgres://bad:bad@127.0.0.1:1/none?connect_timeout=1")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestNewRedisClientSuccess(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	cfg := &config.Config{RedisAddr: mr.Addr()}
	ctx := context.Background()

	client, err := database.NewRedisClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewRedisClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestNewRedisClientFailure(t *testing.T) {
	cfg := &config.Config{RedisAddr: "127.0.0.1:1"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := database.NewRedisClient(ctx, cfg)
	if err == nil {
		t.Fatal("expected redis connection error")
	}
}

func TestNewRedisClientLive(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = os.Getenv("REDIS_ADDR")
	}
	if addr == "" {
		addr = "localhost:6379"
	}

	cfg := &config.Config{RedisAddr: addr}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client, err := database.NewRedisClient(ctx, cfg)
	if err != nil {
		t.Skipf("live redis unavailable at %s: %v", addr, err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
