package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/migus/url_shortener/internal/domain"
	"github.com/migus/url_shortener/internal/shortcode"
)

const maxGenerateAttempts = 5

var shortCodePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,32}$`)

type URLStore interface {
	Create(ctx context.Context, shortCode, longURL string) (*domain.URL, error)
	GetByCode(ctx context.Context, shortCode string) (*domain.URL, error)
	Update(ctx context.Context, currentCode, newCode, longURL string) (*domain.URL, error)
	Delete(ctx context.Context, shortCode string) error
	IncrementClicks(ctx context.Context, shortCode string) error
}

type URLCacher interface {
	Get(ctx context.Context, shortCode string) (string, bool, error)
	Set(ctx context.Context, shortCode, longURL string) error
	Delete(ctx context.Context, shortCode string) error
}

type URLService struct {
	store URLStore
	cache URLCacher
	log   *slog.Logger
}

func NewURLService(store URLStore, cache URLCacher, log *slog.Logger) *URLService {
	if log == nil {
		log = slog.Default()
	}
	return &URLService{store: store, cache: cache, log: log}
}

type CreateInput struct {
	LongURL    string
	CustomCode string
}

type UpdateInput struct {
	LongURL    *string
	CustomCode *string
}

func (s *URLService) Create(ctx context.Context, in CreateInput) (*domain.URL, error) {
	longURL, err := validateLongURL(in.LongURL)
	if err != nil {
		return nil, err
	}

	code := strings.TrimSpace(in.CustomCode)
	if code != "" {
		if err := validateShortCode(code); err != nil {
			return nil, err
		}
		u, err := s.store.Create(ctx, code, longURL)
		if err != nil {
			return nil, err
		}
		_ = s.cache.Set(ctx, u.ShortCode, u.LongURL)
		return u, nil
	}

	for i := 0; i < maxGenerateAttempts; i++ {
		generated, err := shortcode.Generate()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrGenerateCode, err)
		}
		u, err := s.store.Create(ctx, generated, longURL)
		if err != nil {
			if errors.Is(err, domain.ErrConflict) {
				continue
			}
			return nil, err
		}
		_ = s.cache.Set(ctx, u.ShortCode, u.LongURL)
		return u, nil
	}

	return nil, domain.ErrGenerateCode
}

func (s *URLService) Get(ctx context.Context, code string) (*domain.URL, error) {
	if err := validateShortCode(code); err != nil {
		return nil, err
	}
	return s.store.GetByCode(ctx, code)
}

func (s *URLService) Update(ctx context.Context, code string, in UpdateInput) (*domain.URL, error) {
	if err := validateShortCode(code); err != nil {
		return nil, err
	}

	current, err := s.store.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}

	newCode := current.ShortCode
	if in.CustomCode != nil {
		candidate := strings.TrimSpace(*in.CustomCode)
		if candidate == "" {
			return nil, domain.ErrInvalidCode
		}
		if err := validateShortCode(candidate); err != nil {
			return nil, err
		}
		newCode = candidate
	}

	longURL := current.LongURL
	if in.LongURL != nil {
		validated, err := validateLongURL(*in.LongURL)
		if err != nil {
			return nil, err
		}
		longURL = validated
	}

	updated, err := s.store.Update(ctx, current.ShortCode, newCode, longURL)
	if err != nil {
		return nil, err
	}

	if current.ShortCode != updated.ShortCode {
		_ = s.cache.Delete(ctx, current.ShortCode)
	}
	_ = s.cache.Set(ctx, updated.ShortCode, updated.LongURL)

	return updated, nil
}

func (s *URLService) Delete(ctx context.Context, code string) error {
	if err := validateShortCode(code); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, code); err != nil {
		return err
	}
	_ = s.cache.Delete(ctx, code)
	return nil
}

// ResolveRedirect looks up the long URL cache-first, then Postgres.
// Click counting runs asynchronously and must not block the redirect.
func (s *URLService) ResolveRedirect(ctx context.Context, code string) (string, error) {
	if err := validateShortCode(code); err != nil {
		return "", err
	}

	if longURL, hit, err := s.cache.Get(ctx, code); err != nil {
		s.log.Warn("cache get failed, falling back to db", "code", code, "err", err)
	} else if hit {
		s.incrementClicksAsync(code)
		return longURL, nil
	}

	u, err := s.store.GetByCode(ctx, code)
	if err != nil {
		return "", err
	}

	if err := s.cache.Set(ctx, u.ShortCode, u.LongURL); err != nil {
		s.log.Warn("cache set failed after db hit", "code", code, "err", err)
	}

	s.incrementClicksAsync(code)
	return u.LongURL, nil
}

func (s *URLService) incrementClicksAsync(code string) {
	go func() {
		ctx := context.Background()
		if err := s.store.IncrementClicks(ctx, code); err != nil {
			s.log.Warn("async click increment failed", "code", code, "err", err)
		}
	}()
}

func validateLongURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", domain.ErrInvalidURL
	}

	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return "", domain.ErrInvalidURL
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", domain.ErrInvalidURL
	}
	if u.Host == "" {
		return "", domain.ErrInvalidURL
	}
	return raw, nil
}

func validateShortCode(code string) error {
	code = strings.TrimSpace(code)
	if !shortCodePattern.MatchString(code) {
		return domain.ErrInvalidCode
	}
	return nil
}
