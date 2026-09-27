package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerAddr          string
	DatabaseURL         string
	RedisAddr           string
	RedisPassword       string
	RedisDB             int
	CacheTTL            time.Duration
	APIKeys             []string
	RateLimitRedirect   int
	RateLimitAPI        int
	RateLimitWindow     time.Duration
}

func Load() (*Config, error) {
	cacheTTL, err := parseDuration(getEnv("CACHE_TTL", "24h"))
	if err != nil {
		return nil, fmt.Errorf("CACHE_TTL: %w", err)
	}

	rateWindow, err := parseDuration(getEnv("RATE_LIMIT_WINDOW", "1m"))
	if err != nil {
		return nil, fmt.Errorf("RATE_LIMIT_WINDOW: %w", err)
	}

	redisDB, err := strconv.Atoi(getEnv("REDIS_DB", "0"))
	if err != nil {
		return nil, fmt.Errorf("REDIS_DB: %w", err)
	}

	redirectLimit, err := strconv.Atoi(getEnv("RATE_LIMIT_REDIRECT", "120"))
	if err != nil {
		return nil, fmt.Errorf("RATE_LIMIT_REDIRECT: %w", err)
	}

	apiLimit, err := strconv.Atoi(getEnv("RATE_LIMIT_API", "60"))
	if err != nil {
		return nil, fmt.Errorf("RATE_LIMIT_API: %w", err)
	}

	apiKeysRaw := getEnv("API_KEYS", "")
	if apiKeysRaw == "" {
		return nil, fmt.Errorf("API_KEYS is required")
	}

	keys := splitAndTrim(apiKeysRaw)
	if len(keys) == 0 {
		return nil, fmt.Errorf("API_KEYS must contain at least one key")
	}

	databaseURL := getEnv("DATABASE_URL", "")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	return &Config{
		ServerAddr:        getEnv("SERVER_ADDR", ":8080"),
		DatabaseURL:       databaseURL,
		RedisAddr:         getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:     getEnv("REDIS_PASSWORD", ""),
		RedisDB:           redisDB,
		CacheTTL:          cacheTTL,
		APIKeys:           keys,
		RateLimitRedirect: redirectLimit,
		RateLimitAPI:      apiLimit,
		RateLimitWindow:   rateWindow,
	}, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	return d, nil
}
