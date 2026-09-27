package middleware

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type RateLimiter interface {
	Allow(ctx context.Context, key string, limit int) (bool, error)
}

type RateLimitConfig struct {
	Limiter    RateLimiter
	Limit      int
	WindowSecs int
	KeyFunc    func(c *gin.Context) string
}

func RateLimit(cfg RateLimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := cfg.KeyFunc(c)
		allowed, err := cfg.Limiter.Allow(c.Request.Context(), key, cfg.Limit)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "rate limiter unavailable"})
			return
		}
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(cfg.WindowSecs))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		c.Next()
	}
}

func ClientIPKey(prefix string) func(c *gin.Context) string {
	return func(c *gin.Context) string {
		return prefix + ":" + c.ClientIP()
	}
}

func APIKeyRateKey(prefix string) func(c *gin.Context) string {
	return func(c *gin.Context) string {
		key := c.GetHeader(APIKeyHeader)
		if key == "" {
			key = c.ClientIP()
		}
		return prefix + ":" + key
	}
}
