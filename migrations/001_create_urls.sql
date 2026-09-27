CREATE TABLE IF NOT EXISTS urls (
    id          BIGSERIAL PRIMARY KEY,
    short_code  VARCHAR(32) NOT NULL,
    long_url    TEXT NOT NULL,
    click_count BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT urls_short_code_unique UNIQUE (short_code)
);

CREATE INDEX IF NOT EXISTS idx_urls_short_code ON urls (short_code);
