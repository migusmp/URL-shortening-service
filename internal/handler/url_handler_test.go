package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/migus/url_shortener/internal/domain"
	"github.com/migus/url_shortener/internal/handler"
	"github.com/migus/url_shortener/internal/service"
)

type mockStore struct {
	mu   sync.Mutex
	urls map[string]*domain.URL
	next int64
}

func newMockStore() *mockStore {
	return &mockStore{urls: make(map[string]*domain.URL)}
}

func (m *mockStore) Create(_ context.Context, shortCode, longURL string) (*domain.URL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.urls[shortCode]; ok {
		return nil, domain.ErrConflict
	}
	m.next++
	u := &domain.URL{
		ID: m.next, ShortCode: shortCode, LongURL: longURL,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	m.urls[shortCode] = u
	cp := *u
	return &cp, nil
}

func (m *mockStore) GetByCode(_ context.Context, shortCode string) (*domain.URL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.urls[shortCode]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (m *mockStore) Update(_ context.Context, currentCode, newCode, longURL string) (*domain.URL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.urls[currentCode]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if currentCode != newCode {
		if _, exists := m.urls[newCode]; exists {
			return nil, domain.ErrConflict
		}
		delete(m.urls, currentCode)
	}
	u.ShortCode = newCode
	u.LongURL = longURL
	m.urls[newCode] = u
	cp := *u
	return &cp, nil
}

func (m *mockStore) Delete(_ context.Context, shortCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.urls[shortCode]; !ok {
		return domain.ErrNotFound
	}
	delete(m.urls, shortCode)
	return nil
}

func (m *mockStore) IncrementClicks(_ context.Context, shortCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.urls[shortCode]; ok {
		u.ClickCount++
	}
	return nil
}

type mockCache struct {
	mu   sync.Mutex
	data map[string]string
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]string)}
}

func (c *mockCache) Get(_ context.Context, code string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.data[code]
	return v, ok, nil
}

func (c *mockCache) Set(_ context.Context, code, longURL string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[code] = longURL
	return nil
}

func (c *mockCache) Delete(_ context.Context, code string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, code)
	return nil
}

func newHandler() *handler.URLHandler {
	svc := service.NewURLService(newMockStore(), newMockCache(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return handler.NewURLHandler(svc)
}

func setupRouter(h *handler.URLHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/health", h.Health)
	r.POST("/urls", h.Create)
	r.GET("/urls/:code", h.Get)
	r.PUT("/urls/:code", h.Update)
	r.DELETE("/urls/:code", h.Delete)
	r.GET("/:code", h.Redirect)
	return r
}

func TestHealth(t *testing.T) {
	r := setupRouter(newHandler())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestCreateAndGet(t *testing.T) {
	r := setupRouter(newHandler())

	body := `{"long_url":"https://example.com","custom_code":"docs"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/urls", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}

	var created domain.URL
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ShortCode != "docs" {
		t.Fatalf("short_code = %q", created.ShortCode)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/urls/docs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d", w.Code)
	}
}

func TestCreateInvalidBody(t *testing.T) {
	r := setupRouter(newHandler())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/urls", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestCreateConflict(t *testing.T) {
	r := setupRouter(newHandler())
	body := `{"long_url":"https://example.com","custom_code":"dup"}`
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/urls", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if i == 0 && w.Code != http.StatusCreated {
			t.Fatalf("first create: %d", w.Code)
		}
		if i == 1 && w.Code != http.StatusConflict {
			t.Fatalf("second create: %d", w.Code)
		}
	}
}

func TestUpdateAndDelete(t *testing.T) {
	r := setupRouter(newHandler())

	create := `{"long_url":"https://example.com","custom_code":"upd"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/urls", bytes.NewBufferString(create))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	update := `{"long_url":"https://example.com/new"}`
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/urls/upd", bytes.NewBufferString(update))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", w.Code, w.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPut, "/urls/upd", bytes.NewBufferString(`{}`))
	req2.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req2)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty update status = %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/urls/upd", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/urls/upd", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete = %d", w.Code)
	}
}

func TestRedirect(t *testing.T) {
	r := setupRouter(newHandler())

	body := `{"long_url":"https://example.com/go","custom_code":"goo"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/urls", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/goo", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("redirect status = %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "https://example.com/go" {
		t.Fatalf("Location = %q", loc)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/missingcode", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing redirect = %d", w.Code)
	}
}

func TestGetNotFound(t *testing.T) {
	r := setupRouter(newHandler())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/urls/zzz", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}
}
