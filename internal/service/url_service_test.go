package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/migus/url_shortener/internal/domain"
	"github.com/migus/url_shortener/internal/service"
)

type mockStore struct {
	mu    sync.Mutex
	urls  map[string]*domain.URL
	next  int64
	clicks map[string]int64
}

func newMockStore() *mockStore {
	return &mockStore{
		urls:   make(map[string]*domain.URL),
		clicks: make(map[string]int64),
	}
}

func (m *mockStore) Create(_ context.Context, shortCode, longURL string) (*domain.URL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.urls[shortCode]; exists {
		return nil, domain.ErrConflict
	}
	m.next++
	u := &domain.URL{
		ID:         m.next,
		ShortCode:  shortCode,
		LongURL:    longURL,
		ClickCount: 0,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	m.urls[shortCode] = u
	return cloneURL(u), nil
}

func (m *mockStore) GetByCode(_ context.Context, shortCode string) (*domain.URL, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.urls[shortCode]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return cloneURL(u), nil
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
	u.UpdatedAt = time.Now().UTC()
	m.urls[newCode] = u
	return cloneURL(u), nil
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
	u, ok := m.urls[shortCode]
	if !ok {
		return domain.ErrNotFound
	}
	u.ClickCount++
	m.clicks[shortCode]++
	return nil
}

type mockCache struct {
	mu   sync.Mutex
	data map[string]string
	gets int
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]string)}
}

func (c *mockCache) Get(_ context.Context, shortCode string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gets++
	v, ok := c.data[shortCode]
	return v, ok, nil
}

func (c *mockCache) Set(_ context.Context, shortCode, longURL string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[shortCode] = longURL
	return nil
}

func (c *mockCache) Delete(_ context.Context, shortCode string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, shortCode)
	return nil
}

func cloneURL(u *domain.URL) *domain.URL {
	cp := *u
	return &cp
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCreateWithCustomCode(t *testing.T) {
	store := newMockStore()
	cache := newMockCache()
	svc := service.NewURLService(store, cache, silentLogger())

	u, err := svc.Create(context.Background(), service.CreateInput{
		LongURL:    "https://example.com/path",
		CustomCode: "my-link",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ShortCode != "my-link" {
		t.Fatalf("expected my-link, got %s", u.ShortCode)
	}
	if cache.data["my-link"] != "https://example.com/path" {
		t.Fatalf("expected cache set")
	}
}

func TestCreateRejectsInvalidURL(t *testing.T) {
	svc := service.NewURLService(newMockStore(), newMockCache(), silentLogger())
	_, err := svc.Create(context.Background(), service.CreateInput{LongURL: "ftp://bad"})
	if !errors.Is(err, domain.ErrInvalidURL) {
		t.Fatalf("expected ErrInvalidURL, got %v", err)
	}
}

func TestCreateConflict(t *testing.T) {
	store := newMockStore()
	svc := service.NewURLService(store, newMockCache(), silentLogger())
	_, err := svc.Create(context.Background(), service.CreateInput{
		LongURL:    "https://example.com",
		CustomCode: "taken",
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err = svc.Create(context.Background(), service.CreateInput{
		LongURL:    "https://example.com/other",
		CustomCode: "taken",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestResolveRedirectCacheFirst(t *testing.T) {
	store := newMockStore()
	cache := newMockCache()
	svc := service.NewURLService(store, cache, silentLogger())

	_, err := svc.Create(context.Background(), service.CreateInput{
		LongURL:    "https://example.com/cached",
		CustomCode: "abc123",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Remove from store to prove redirect comes from cache
	store.mu.Lock()
	delete(store.urls, "abc123")
	store.mu.Unlock()

	longURL, err := svc.ResolveRedirect(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if longURL != "https://example.com/cached" {
		t.Fatalf("unexpected url: %s", longURL)
	}
	if cache.gets < 1 {
		t.Fatalf("expected cache get")
	}
}

func TestResolveRedirectFallsBackToDB(t *testing.T) {
	store := newMockStore()
	cache := newMockCache()
	svc := service.NewURLService(store, cache, silentLogger())

	_, err := store.Create(context.Background(), "fromdb", "https://example.com/db")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	longURL, err := svc.ResolveRedirect(context.Background(), "fromdb")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if longURL != "https://example.com/db" {
		t.Fatalf("unexpected url: %s", longURL)
	}
	if cache.data["fromdb"] != "https://example.com/db" {
		t.Fatalf("expected cache populate after db miss")
	}
}

func TestUpdateAndDeleteInvalidateCache(t *testing.T) {
	store := newMockStore()
	cache := newMockCache()
	svc := service.NewURLService(store, cache, silentLogger())

	_, err := svc.Create(context.Background(), service.CreateInput{
		LongURL:    "https://example.com/old",
		CustomCode: "oldcode",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	newCode := "newcode"
	newURL := "https://example.com/new"
	_, err = svc.Update(context.Background(), "oldcode", service.UpdateInput{
		LongURL:    &newURL,
		CustomCode: &newCode,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, ok := cache.data["oldcode"]; ok {
		t.Fatalf("old code should be invalidated")
	}
	if cache.data["newcode"] != newURL {
		t.Fatalf("new code should be cached")
	}

	if err := svc.Delete(context.Background(), "newcode"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := cache.data["newcode"]; ok {
		t.Fatalf("deleted code should be removed from cache")
	}
}

func TestCreateGeneratesCode(t *testing.T) {
	svc := service.NewURLService(newMockStore(), newMockCache(), silentLogger())
	u, err := svc.Create(context.Background(), service.CreateInput{
		LongURL: "https://example.com/auto",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(u.ShortCode) != 7 {
		t.Fatalf("expected 7-char code, got %q", u.ShortCode)
	}
}
