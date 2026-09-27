package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/joho/godotenv"
	"github.com/migus/url_shortener/internal/cache"
	"github.com/migus/url_shortener/internal/config"
	"github.com/migus/url_shortener/internal/database"
	"github.com/migus/url_shortener/internal/handler"
	"github.com/migus/url_shortener/internal/repository"
	"github.com/migus/url_shortener/internal/server"
	"github.com/migus/url_shortener/internal/service"
)

func main() {
	_ = godotenv.Load()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()

	pool, err := database.NewPostgresPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	rdb, err := database.NewRedisClient(ctx, cfg)
	if err != nil {
		log.Error("redis connect failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := rdb.Close(); err != nil {
			log.Error("redis close failed", "err", err)
		}
	}()

	urlCache := cache.NewURLCache(rdb, cfg.CacheTTL)
	rateLimiter := cache.NewRateLimiter(rdb, cfg.RateLimitWindow)
	repo := repository.NewURLRepository(pool)
	svc := service.NewURLService(repo, urlCache, log)
	h := handler.NewURLHandler(svc)

	router := server.NewRouter(server.Dependencies{
		Config:      cfg,
		Handler:     h,
		RateLimiter: rateLimiter,
	})

	if err := server.Run(cfg.ServerAddr, router, log); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}
