package repository_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/migus/url_shortener/internal/domain"
	"github.com/migus/url_shortener/internal/repository"
)

func setupRepo(t *testing.T) (*repository.URLRepository, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL / DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres unavailable: %v", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS urls (
			id          BIGSERIAL PRIMARY KEY,
			short_code  VARCHAR(32) NOT NULL,
			long_url    TEXT NOT NULL,
			click_count BIGINT NOT NULL DEFAULT 0,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CONSTRAINT urls_short_code_unique UNIQUE (short_code)
		)
	`)
	if err != nil {
		pool.Close()
		t.Fatalf("ensure schema: %v", err)
	}

	t.Cleanup(func() { pool.Close() })
	return repository.NewURLRepository(pool), pool
}

func uniqueCode(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano()%1_000_000_000)
}

func TestURLRepositoryCRUD(t *testing.T) {
	repo, _ := setupRepo(t)
	ctx := context.Background()
	code := uniqueCode("crud")

	created, err := repo.Create(ctx, code, "https://example.com/a")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ShortCode != code || created.LongURL != "https://example.com/a" || created.ClickCount != 0 {
		t.Fatalf("unexpected create result: %+v", created)
	}

	got, err := repo.GetByCode(ctx, code)
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("id mismatch")
	}

	updated, err := repo.Update(ctx, code, code+"2", "https://example.com/b")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ShortCode != code+"2" || updated.LongURL != "https://example.com/b" {
		t.Fatalf("unexpected update: %+v", updated)
	}

	if err := repo.IncrementClicks(ctx, code+"2"); err != nil {
		t.Fatalf("IncrementClicks: %v", err)
	}
	got, err = repo.GetByCode(ctx, code+"2")
	if err != nil {
		t.Fatalf("Get after increment: %v", err)
	}
	if got.ClickCount != 1 {
		t.Fatalf("click_count = %d, want 1", got.ClickCount)
	}

	if err := repo.Delete(ctx, code+"2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = repo.GetByCode(ctx, code+"2")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestURLRepositoryConflict(t *testing.T) {
	repo, _ := setupRepo(t)
	ctx := context.Background()
	code := uniqueCode("cfl")

	if _, err := repo.Create(ctx, code, "https://example.com/1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := repo.Create(ctx, code, "https://example.com/2")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestURLRepositoryNotFound(t *testing.T) {
	repo, _ := setupRepo(t)
	ctx := context.Background()
	missing := uniqueCode("miss")

	_, err := repo.GetByCode(ctx, missing)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByCode: %v", err)
	}

	_, err = repo.Update(ctx, missing, missing, "https://example.com")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update: %v", err)
	}

	err = repo.Delete(ctx, missing)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete: %v", err)
	}
}

func TestURLRepositoryUpdateConflict(t *testing.T) {
	repo, _ := setupRepo(t)
	ctx := context.Background()
	a := uniqueCode("upa")
	b := uniqueCode("upb")

	if _, err := repo.Create(ctx, a, "https://example.com/a"); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	if _, err := repo.Create(ctx, b, "https://example.com/b"); err != nil {
		t.Fatalf("Create b: %v", err)
	}

	_, err := repo.Update(ctx, a, b, "https://example.com/x")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}

	_ = repo.Delete(ctx, a)
	_ = repo.Delete(ctx, b)
}
