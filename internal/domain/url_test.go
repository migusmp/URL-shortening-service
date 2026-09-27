package domain_test

import (
	"errors"
	"testing"

	"github.com/migus/url_shortener/internal/domain"
)

func TestDomainErrors(t *testing.T) {
	errs := []error{
		domain.ErrNotFound,
		domain.ErrConflict,
		domain.ErrInvalidURL,
		domain.ErrInvalidCode,
		domain.ErrGenerateCode,
	}
	for _, err := range errs {
		if err == nil || errors.Is(err, nil) {
			t.Fatalf("invalid sentinel: %v", err)
		}
		if err.Error() == "" {
			t.Fatalf("empty message for %v", err)
		}
	}
}

func TestURLStructFields(t *testing.T) {
	u := domain.URL{ID: 1, ShortCode: "abc", LongURL: "https://example.com", ClickCount: 2}
	if u.ShortCode != "abc" || u.ClickCount != 2 {
		t.Fatalf("unexpected url: %+v", u)
	}
}
