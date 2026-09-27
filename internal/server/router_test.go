package server_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/migus/url_shortener/internal/config"
	"github.com/migus/url_shortener/internal/domain"
	"github.com/migus/url_shortener/internal/handler"
	"github.com/migus/url_shortener/internal/server"
	"github.com/migus/url_shortener/internal/service"
)

type mockStore struct {
	mu   sync.Mutex
	urls map[string]*domain.URL
	next int64
}

func newStore() *mockStore {
	return &mockStore{urls: make(map[string]*domain.URL)}
}

func (m *mockStore) Create(_ context.Context, code, longURL string) (*domain.URL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.urls[code]; ok {
		return nil, domain.ErrConflict
	}
	m.next++
	u := &domain.URL{ID: m.next, ShortCode: code, LongURL: longURL, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	m.urls[code] = u
	cp := *u
	return &cp, nil
}
func (m *mockStore) GetByCode(_ context.Context, code string) (*domain.URL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.urls[code]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *u
	return &cp, nil
}
func (m *mockStore) Update(_ context.Context, cur, neu, longURL string) (*domain.URL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.urls[cur]
	if !ok {
		return nil, domain.ErrNotFound
	}
	delete(m.urls, cur)
	u.ShortCode = neu
	u.LongURL = longURL
	m.urls[neu] = u
	cp := *u
	return &cp, nil
}
func (m *mockStore) Delete(_ context.Context, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.urls[code]; !ok {
		return domain.ErrNotFound
	}
	delete(m.urls, code)
	return nil
}
func (m *mockStore) IncrementClicks(_ context.Context, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.urls[code]; ok {
		u.ClickCount++
	}
	return nil
}

type mockCache struct {
	data map[string]string
	mu   sync.Mutex
}

func (c *mockCache) Get(_ context.Context, code string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.data[code]
	return v, ok, nil
}
func (c *mockCache) Set(_ context.Context, code, url string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		c.data = map[string]string{}
	}
	c.data[code] = url
	return nil
}
func (c *mockCache) Delete(_ context.Context, code string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, code)
	return nil
}

type allowAll struct{}

func (allowAll) Allow(context.Context, string, int) (bool, error) { return true, nil }

func TestNewRouterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	svc := service.NewURLService(newStore(), &mockCache{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := handler.NewURLHandler(svc)
	cfg := &config.Config{
		APIKeys:           []string{"test-key"},
		RateLimitAPI:      100,
		RateLimitRedirect: 100,
		RateLimitWindow:   time.Minute,
	}

	r := server.NewRouter(server.Dependencies{
		Config:      cfg,
		Handler:     h,
		RateLimiter: allowAll{},
	})

	t.Run("health", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("api without key", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/urls", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("api with key create and redirect", func(t *testing.T) {
		body := `{"long_url":"https://example.com","custom_code":"route"}`
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/urls", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "test-key")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
		}

		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/route", nil))
		if w.Code != http.StatusFound {
			t.Fatalf("redirect status = %d", w.Code)
		}
	})
}

func TestRateWindowSecondsFallback(t *testing.T) {
	// Indirectly verify NewRouter accepts sub-second windows without panicking.
	gin.SetMode(gin.TestMode)
	svc := service.NewURLService(newStore(), &mockCache{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := server.NewRouter(server.Dependencies{
		Config: &config.Config{
			APIKeys:           []string{"k"},
			RateLimitAPI:      1,
			RateLimitRedirect: 1,
			RateLimitWindow:   time.Millisecond,
		},
		Handler:     handler.NewURLHandler(svc),
		RateLimiter: allowAll{},
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}
