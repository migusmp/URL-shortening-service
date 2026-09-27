package shortcode_test

import (
	"testing"

	"github.com/migus/url_shortener/internal/shortcode"
)

func TestGenerateDefaultLength(t *testing.T) {
	code, err := shortcode.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(code) != 7 {
		t.Fatalf("len = %d, want 7 (%q)", len(code), code)
	}
	assertAlphabet(t, code)
}

func TestGenerateN(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want int
	}{
		{"positive", 10, 10},
		{"zero defaults", 0, 7},
		{"negative defaults", -3, 7},
		{"one", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := shortcode.GenerateN(tt.n)
			if err != nil {
				t.Fatalf("GenerateN: %v", err)
			}
			if len(code) != tt.want {
				t.Fatalf("len = %d, want %d", len(code), tt.want)
			}
			assertAlphabet(t, code)
		})
	}
}

func TestGenerateUniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		code, err := shortcode.Generate()
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		seen[code] = struct{}{}
	}
	if len(seen) < 95 {
		t.Fatalf("expected high uniqueness, got %d unique codes", len(seen))
	}
}

func assertAlphabet(t *testing.T, code string) {
	t.Helper()
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	for _, c := range code {
		found := false
		for _, a := range alphabet {
			if c == a {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("char %q not in alphabet", c)
		}
	}
}
