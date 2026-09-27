package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/migus/url_shortener/internal/cache"
	"github.com/migus/url_shortener/internal/config"
	"github.com/migus/url_shortener/internal/handler"
	"github.com/migus/url_shortener/internal/middleware"
	"github.com/migus/url_shortener/internal/repository"
	"github.com/migus/url_shortener/internal/service"
	"github.com/redis/go-redis/v9"
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

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Error("postgres ping failed", "err", err)
		os.Exit(1)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Error("redis ping failed", "err", err)
		os.Exit(1)
	}

	urlCache := cache.NewURLCache(rdb, cfg.CacheTTL)
	rateLimiter := cache.NewRateLimiter(rdb, cfg.RateLimitWindow)
	repo := repository.NewURLRepository(pool)
	svc := service.NewURLService(repo, urlCache, log)
	h := handler.NewURLHandler(svc)

	windowSecs := int(cfg.RateLimitWindow.Seconds())
	if windowSecs < 1 {
		windowSecs = 60
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	r.GET("/health", h.Health)

	api := r.Group("/api/v1")
	api.Use(middleware.APIKeyAuth(cfg.APIKeys))
	api.Use(middleware.RateLimit(middleware.RateLimitConfig{
		Limiter:    rateLimiter,
		Limit:      cfg.RateLimitAPI,
		WindowSecs: windowSecs,
		KeyFunc:    middleware.APIKeyRateKey("api"),
	}))
	{
		api.POST("/urls", h.Create)
		api.GET("/urls/:code", h.Get)
		api.PUT("/urls/:code", h.Update)
		api.DELETE("/urls/:code", h.Delete)
	}

	r.GET("/:code",
		middleware.RateLimit(middleware.RateLimitConfig{
			Limiter:    rateLimiter,
			Limit:      cfg.RateLimitRedirect,
			WindowSecs: windowSecs,
			KeyFunc:    middleware.ClientIPKey("redirect"),
		}),
		h.Redirect,
	)

	srv := &http.Server{
		Addr:              cfg.ServerAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("server starting", "addr", cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown failed", "err", err)
	}
	log.Info("server stopped")
}
