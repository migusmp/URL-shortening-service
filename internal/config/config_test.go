package config_test

import (
	"testing"
	"time"

	"github.com/migus/url_shortener/internal/config"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db?sslmode=disable")
	t.Setenv("API_KEYS", "key-one, key-two")
}

func TestLoadDefaults(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ServerAddr != ":8080" {
		t.Fatalf("ServerAddr = %q", cfg.ServerAddr)
	}
	if cfg.RedisAddr != "localhost:6379" {
		t.Fatalf("RedisAddr = %q", cfg.RedisAddr)
	}
	if cfg.RedisDB != 0 {
		t.Fatalf("RedisDB = %d", cfg.RedisDB)
	}
	if cfg.CacheTTL != 24*time.Hour {
		t.Fatalf("CacheTTL = %v", cfg.CacheTTL)
	}
	if cfg.RateLimitRedirect != 120 || cfg.RateLimitAPI != 60 {
		t.Fatalf("rate limits = %d/%d", cfg.RateLimitRedirect, cfg.RateLimitAPI)
	}
	if cfg.RateLimitWindow != time.Minute {
		t.Fatalf("RateLimitWindow = %v", cfg.RateLimitWindow)
	}
	if len(cfg.APIKeys) != 2 || cfg.APIKeys[0] != "key-one" || cfg.APIKeys[1] != "key-two" {
		t.Fatalf("APIKeys = %#v", cfg.APIKeys)
	}
}

func TestLoadCustomValues(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("SERVER_ADDR", ":9090")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("CACHE_TTL", "1h")
	t.Setenv("RATE_LIMIT_REDIRECT", "10")
	t.Setenv("RATE_LIMIT_API", "5")
	t.Setenv("RATE_LIMIT_WINDOW", "30s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ServerAddr != ":9090" || cfg.RedisPassword != "secret" || cfg.RedisDB != 2 {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if cfg.CacheTTL != time.Hour || cfg.RateLimitWindow != 30*time.Second {
		t.Fatalf("unexpected durations: %+v", cfg)
	}
	if cfg.RateLimitRedirect != 10 || cfg.RateLimitAPI != 5 {
		t.Fatalf("unexpected limits: %+v", cfg)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	t.Run("missing API_KEYS", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("API_KEYS", "")
		if _, err := config.Load(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("blank API_KEYS commas", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("API_KEYS", " , , ")
		if _, err := config.Load(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing DATABASE_URL", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		t.Setenv("API_KEYS", "k")
		if _, err := config.Load(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestLoadInvalidValues(t *testing.T) {
	setRequiredEnv(t)

	cases := []struct {
		key, value string
	}{
		{"CACHE_TTL", "not-a-duration"},
		{"CACHE_TTL", "0s"},
		{"CACHE_TTL", "-1h"},
		{"RATE_LIMIT_WINDOW", "bad"},
		{"REDIS_DB", "x"},
		{"RATE_LIMIT_REDIRECT", "x"},
		{"RATE_LIMIT_API", "x"},
	}

	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := config.Load(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
