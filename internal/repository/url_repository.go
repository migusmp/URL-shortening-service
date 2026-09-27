package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/migus/url_shortener/internal/domain"
)

type URLRepository struct {
	pool *pgxpool.Pool
}

func NewURLRepository(pool *pgxpool.Pool) *URLRepository {
	return &URLRepository{pool: pool}
}

func (r *URLRepository) Create(ctx context.Context, shortCode, longURL string) (*domain.URL, error) {
	const q = `
		INSERT INTO urls (short_code, long_url)
		VALUES ($1, $2)
		RETURNING id, short_code, long_url, click_count, created_at, updated_at
	`

	var u domain.URL
	err := r.pool.QueryRow(ctx, q, shortCode, longURL).Scan(
		&u.ID, &u.ShortCode, &u.LongURL, &u.ClickCount, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("create url: %w", err)
	}
	return &u, nil
}

func (r *URLRepository) GetByCode(ctx context.Context, shortCode string) (*domain.URL, error) {
	const q = `
		SELECT id, short_code, long_url, click_count, created_at, updated_at
		FROM urls
		WHERE short_code = $1
	`

	var u domain.URL
	err := r.pool.QueryRow(ctx, q, shortCode).Scan(
		&u.ID, &u.ShortCode, &u.LongURL, &u.ClickCount, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get url: %w", err)
	}
	return &u, nil
}

func (r *URLRepository) Update(ctx context.Context, currentCode, newCode, longURL string) (*domain.URL, error) {
	const q = `
		UPDATE urls
		SET short_code = $1,
		    long_url = $2,
		    updated_at = $3
		WHERE short_code = $4
		RETURNING id, short_code, long_url, click_count, created_at, updated_at
	`

	now := time.Now().UTC()
	var u domain.URL
	err := r.pool.QueryRow(ctx, q, newCode, longURL, now, currentCode).Scan(
		&u.ID, &u.ShortCode, &u.LongURL, &u.ClickCount, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		if isUniqueViolation(err) {
			return nil, domain.ErrConflict
		}
		return nil, fmt.Errorf("update url: %w", err)
	}
	return &u, nil
}

func (r *URLRepository) Delete(ctx context.Context, shortCode string) error {
	const q = `DELETE FROM urls WHERE short_code = $1`

	tag, err := r.pool.Exec(ctx, q, shortCode)
	if err != nil {
		return fmt.Errorf("delete url: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *URLRepository) IncrementClicks(ctx context.Context, shortCode string) error {
	const q = `
		UPDATE urls
		SET click_count = click_count + 1,
		    updated_at = $1
		WHERE short_code = $2
	`

	_, err := r.pool.Exec(ctx, q, time.Now().UTC(), shortCode)
	if err != nil {
		return fmt.Errorf("increment clicks: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
