package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/migus/url_shortener/internal/middleware"
)

type stubLimiter struct {
	allow bool
	err   error
	calls int
	last  string
}

func (s *stubLimiter) Allow(_ context.Context, key string, _ int) (bool, error) {
	s.calls++
	s.last = key
	return s.allow, s.err
}

func TestAPIKeyAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.APIKeyAuth([]string{"secret-key"}))
	r.GET("/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	t.Run("missing key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(middleware.APIKeyHeader, "wrong")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})

	t.Run("valid key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(middleware.APIKeyHeader, "secret-key")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("different length key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set(middleware.APIKeyHeader, "short")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
	})
}

func TestRateLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("allows", func(t *testing.T) {
		lim := &stubLimiter{allow: true}
		r := gin.New()
		r.Use(middleware.RateLimit(middleware.RateLimitConfig{
			Limiter:    lim,
			Limit:      5,
			WindowSecs: 60,
			KeyFunc:    middleware.ClientIPKey("redirect"),
		}))
		r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if lim.calls != 1 || lim.last == "" {
			t.Fatalf("limiter not called properly: %+v", lim)
		}
	})

	t.Run("denies", func(t *testing.T) {
		lim := &stubLimiter{allow: false}
		r := gin.New()
		r.Use(middleware.RateLimit(middleware.RateLimitConfig{
			Limiter:    lim,
			Limit:      1,
			WindowSecs: 30,
			KeyFunc:    middleware.ClientIPKey("redirect"),
		}))
		r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429, got %d", w.Code)
		}
		if w.Header().Get("Retry-After") != "30" {
			t.Fatalf("Retry-After = %q", w.Header().Get("Retry-After"))
		}
	})

	t.Run("limiter error", func(t *testing.T) {
		lim := &stubLimiter{err: errors.New("redis down")}
		r := gin.New()
		r.Use(middleware.RateLimit(middleware.RateLimitConfig{
			Limiter:    lim,
			Limit:      1,
			WindowSecs: 60,
			KeyFunc:    middleware.ClientIPKey("redirect"),
		}))
		r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})
}

func TestKeyFuncs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("ClientIPKey", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Request.RemoteAddr = "1.2.3.4:1234"

		key := middleware.ClientIPKey("redirect")(c)
		if key != "redirect:1.2.3.4" {
			t.Fatalf("key = %q", key)
		}
	})

	t.Run("APIKeyRateKey with header", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Request.Header.Set(middleware.APIKeyHeader, "abc")

		key := middleware.APIKeyRateKey("api")(c)
		if key != "api:abc" {
			t.Fatalf("key = %q", key)
		}
	})

	t.Run("APIKeyRateKey fallback IP", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Request.RemoteAddr = "9.9.9.9:1"

		key := middleware.APIKeyRateKey("api")(c)
		if key != "api:9.9.9.9" {
			t.Fatalf("key = %q", key)
		}
	})
}
