package domain

import (
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("url not found")
	ErrConflict      = errors.New("short code already exists")
	ErrInvalidURL    = errors.New("invalid long url")
	ErrInvalidCode   = errors.New("invalid short code")
	ErrGenerateCode  = errors.New("failed to generate unique short code")
)

type URL struct {
	ID         int64     `json:"id"`
	ShortCode  string    `json:"short_code"`
	LongURL    string    `json:"long_url"`
	ClickCount int64     `json:"click_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
