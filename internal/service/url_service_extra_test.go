package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/migus/url_shortener/internal/domain"
	"github.com/migus/url_shortener/internal/service"
)

func TestGetAndDeleteValidation(t *testing.T) {
	svc := service.NewURLService(newMockStore(), newMockCache(), silentLogger())

	_, err := svc.Get(context.Background(), "ab")
	if !errors.Is(err, domain.ErrInvalidCode) {
		t.Fatalf("Get short code: %v", err)
	}

	err = svc.Delete(context.Background(), "!!")
	if !errors.Is(err, domain.ErrInvalidCode) {
		t.Fatalf("Delete invalid: %v", err)
	}

	err = svc.Delete(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestGetSuccess(t *testing.T) {
	store := newMockStore()
	svc := service.NewURLService(store, newMockCache(), silentLogger())
	_, err := svc.Create(context.Background(), service.CreateInput{
		LongURL: "https://example.com", CustomCode: "getme",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	u, err := svc.Get(context.Background(), "getme")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if u.ShortCode != "getme" {
		t.Fatalf("code = %q", u.ShortCode)
	}
}

func TestUpdateValidation(t *testing.T) {
	store := newMockStore()
	svc := service.NewURLService(store, newMockCache(), silentLogger())
	_, _ = svc.Create(context.Background(), service.CreateInput{
		LongURL: "https://example.com", CustomCode: "upd1",
	})

	empty := ""
	_, err := svc.Update(context.Background(), "upd1", service.UpdateInput{CustomCode: &empty})
	if !errors.Is(err, domain.ErrInvalidCode) {
		t.Fatalf("empty custom: %v", err)
	}

	badURL := "not-a-url"
	_, err = svc.Update(context.Background(), "upd1", service.UpdateInput{LongURL: &badURL})
	if !errors.Is(err, domain.ErrInvalidURL) {
		t.Fatalf("bad url: %v", err)
	}

	_, err = svc.Update(context.Background(), "nope", service.UpdateInput{LongURL: ptr("https://example.com")})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}

	_, err = svc.Update(context.Background(), "x", service.UpdateInput{LongURL: ptr("https://example.com")})
	if !errors.Is(err, domain.ErrInvalidCode) {
		t.Fatalf("invalid code: %v", err)
	}
}

func TestUpdateLongURLOnly(t *testing.T) {
	store := newMockStore()
	cache := newMockCache()
	svc := service.NewURLService(store, cache, silentLogger())
	_, _ = svc.Create(context.Background(), service.CreateInput{
		LongURL: "https://example.com/old", CustomCode: "same",
	})

	newURL := "https://example.com/only-url"
	u, err := svc.Update(context.Background(), "same", service.UpdateInput{LongURL: &newURL})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if u.ShortCode != "same" || u.LongURL != newURL {
		t.Fatalf("unexpected: %+v", u)
	}
	if cache.data["same"] != newURL {
		t.Fatalf("cache not updated")
	}
}

func TestResolveRedirectInvalidAndMissing(t *testing.T) {
	svc := service.NewURLService(newMockStore(), newMockCache(), silentLogger())

	_, err := svc.ResolveRedirect(context.Background(), "ab")
	if !errors.Is(err, domain.ErrInvalidCode) {
		t.Fatalf("invalid: %v", err)
	}

	_, err = svc.ResolveRedirect(context.Background(), "nouser")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestResolveRedirectCacheErrorFallsBack(t *testing.T) {
	store := newMockStore()
	_, _ = store.Create(context.Background(), "fbck", "https://example.com/fb")
	svc := service.NewURLService(store, &errCache{}, silentLogger())

	got, err := svc.ResolveRedirect(context.Background(), "fbck")
	if err != nil {
		t.Fatalf("ResolveRedirect: %v", err)
	}
	if got != "https://example.com/fb" {
		t.Fatalf("got %q", got)
	}
}

func TestCreateInvalidCustomCode(t *testing.T) {
	svc := service.NewURLService(newMockStore(), newMockCache(), silentLogger())
	_, err := svc.Create(context.Background(), service.CreateInput{
		LongURL: "https://example.com", CustomCode: "!!",
	})
	if !errors.Is(err, domain.ErrInvalidCode) {
		t.Fatalf("got %v", err)
	}
}

func TestCreateEmptyURL(t *testing.T) {
	svc := service.NewURLService(newMockStore(), newMockCache(), silentLogger())
	_, err := svc.Create(context.Background(), service.CreateInput{LongURL: "   "})
	if !errors.Is(err, domain.ErrInvalidURL) {
		t.Fatalf("got %v", err)
	}
}

func TestNewURLServiceNilLogger(t *testing.T) {
	svc := service.NewURLService(newMockStore(), newMockCache(), nil)
	if svc == nil {
		t.Fatal("expected service")
	}
	_, err := svc.Create(context.Background(), service.CreateInput{
		LongURL: "https://example.com", CustomCode: "nill",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreateConflictRetriesThenSucceeds(t *testing.T) {
	store := &conflictOnceStore{inner: newMockStore(), left: 1}
	svc := service.NewURLService(store, newMockCache(), silentLogger())
	u, err := svc.Create(context.Background(), service.CreateInput{LongURL: "https://example.com/retry"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ShortCode == "" {
		t.Fatal("expected generated code")
	}
}

type errCache struct{}

func (errCache) Get(context.Context, string) (string, bool, error) {
	return "", false, errors.New("cache down")
}
func (errCache) Set(context.Context, string, string) error { return errors.New("cache down") }
func (errCache) Delete(context.Context, string) error      { return nil }

type conflictOnceStore struct {
	inner *mockStore
	left  int
}

func (s *conflictOnceStore) Create(ctx context.Context, code, longURL string) (*domain.URL, error) {
	if s.left > 0 {
		s.left--
		return nil, domain.ErrConflict
	}
	return s.inner.Create(ctx, code, longURL)
}
func (s *conflictOnceStore) GetByCode(ctx context.Context, code string) (*domain.URL, error) {
	return s.inner.GetByCode(ctx, code)
}
func (s *conflictOnceStore) Update(ctx context.Context, a, b, c string) (*domain.URL, error) {
	return s.inner.Update(ctx, a, b, c)
}
func (s *conflictOnceStore) Delete(ctx context.Context, code string) error {
	return s.inner.Delete(ctx, code)
}
func (s *conflictOnceStore) IncrementClicks(ctx context.Context, code string) error {
	return s.inner.IncrementClicks(ctx, code)
}

func ptr(s string) *string { return &s }
