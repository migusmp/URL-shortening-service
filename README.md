# URL Shortener Service

Backend service that turns long URLs into short ones, built with **Go** and the **Gin** framework.

It supports creating, reading, updating, and deleting short URLs (generated or custom codes), high-throughput redirects via **Redis** (cache-first), persistence in **PostgreSQL**, and write-API protection with **API key** auth and **rate limiting**.

---

## Table of contents

1. [What the system does](#what-the-system-does)
2. [Architecture](#architecture)
3. [Tech stack](#tech-stack)
4. [Project structure](#project-structure)
5. [Data model](#data-model)
6. [How it works](#how-it-works)
7. [Cache (Redis)](#cache-redis)
8. [Rate limiting](#rate-limiting)
9. [Authentication](#authentication)
10. [Full API](#full-api)
11. [Validation](#validation)
12. [Error codes](#error-codes)
13. [Local setup](#local-setup)
14. [Environment variables](#environment-variables)
15. [Postman test order](#postman-test-order)
16. [curl examples](#curl-examples)
17. [Tests](#tests)

---

## What the system does

| Capability | Description |
|------------|-------------|
| Shorten URLs | Turns a long URL into a short code (generated or custom) |
| Redirect | `GET /:code` responds with `302` to the original URL |
| Custom codes | Users can choose their own `custom_code` |
| Persistence | Stores `short_code`, `long_url`, and `click_count` in PostgreSQL |
| Cache | Checks Redis before hitting the database |
| Click counter | Each redirect increments `click_count` (asynchronously) |
| CRUD | Create, read, update, and delete short URLs |
| Write security | Management routes require the `X-API-Key` header |
| Rate limiting | Excess requests get a `429` response |

---

## Architecture

```
┌──────────┐     ┌─────────────────────────────────────────────┐
│ Client   │────▶│                   Gin                        │
└──────────┘     │  ┌─────────────┐  ┌──────────────────────┐  │
                 │  │ Rate Limit  │  │ API Key ( /api only) │  │
                 │  └─────────────┘  └──────────────────────┘  │
                 │           │                  │               │
                 │     GET /:code         /api/v1/urls          │
                 │           │                  │               │
                 │           ▼                  ▼               │
                 │      URLService         URLService           │
                 └───────────┬──────────────────┬───────────────┘
                             │                  │
              ┌──────────────┼──────────────────┼──────────────┐
              ▼              ▼                  ▼              │
         ┌────────┐   ┌──────────┐      ┌────────────┐         │
         │ Redis  │   │ Postgres │      │ Redis DEL  │         │
         │ cache  │   │  (CRUD)  │      │ invalidate │         │
         └────────┘   └──────────┘      └────────────┘         │
```

Internal layers:

| Layer | Responsibility |
|-------|----------------|
| `handler` | HTTP: JSON parsing, status codes, redirects |
| `middleware` | API key and rate limit |
| `server` | Route registration and HTTP server lifecycle |
| `service` | Business rules, validation, cache/DB orchestration |
| `repository` | PostgreSQL access (pgx) |
| `database` | Postgres pool and Redis client setup |
| `cache` | Redis access (get/set/del + rate-limit counters) |
| `shortcode` | Random base62 generation |
| `domain` | `URL` entity and domain errors |
| `config` | Environment variable loading |

---

## Tech stack

- **Go 1.22+** — language
- **Gin** — HTTP router/framework
- **PostgreSQL 16** — source of truth
- **Redis 7** — redirect cache + rate limiting
- **pgx** — PostgreSQL driver
- **go-redis** — Redis client
- **godotenv** — loads `.env` locally
- **Docker Compose** — Postgres + Redis

---

## Project structure

```
url_shortener/
├── cmd/server/main.go              # Entrypoint: config, deps wiring
├── internal/
│   ├── config/                     # Env-based configuration
│   ├── database/                   # Postgres + Redis connection helpers
│   ├── domain/                     # URL entity + errors
│   ├── handler/                    # Gin handlers
│   ├── middleware/                 # APIKeyAuth + RateLimit
│   ├── repository/                 # PostgreSQL CRUD
│   ├── cache/                      # Redis URL cache + rate limiter
│   ├── server/                     # Router + HTTP server lifecycle
│   ├── service/                    # Business logic
│   └── shortcode/                  # Base62 generator (7 chars)
├── migrations/
│   └── 001_create_urls.sql         # Initial schema (applied when Postgres starts)
├── docker-compose.yml
├── .env.example
├── go.mod
└── README.md
```

---

## Data model

`urls` table in PostgreSQL:

| Column | Type | Description |
|--------|------|-------------|
| `id` | `BIGSERIAL` | Primary key |
| `short_code` | `VARCHAR(32)` | Unique short code (UNIQUE index) |
| `long_url` | `TEXT` | Original destination URL |
| `click_count` | `BIGINT` | Number of times the short URL was redirected |
| `created_at` | `TIMESTAMPTZ` | Creation timestamp |
| `updated_at` | `TIMESTAMPTZ` | Last update timestamp |

The migration runs automatically when Postgres starts via Docker Compose (`/docker-entrypoint-initdb.d`).

---

## How it works

### 1. Create short URL (`POST /api/v1/urls`)

1. Middleware validates `X-API-Key`.
2. Middleware applies rate limiting per API key.
3. Validates that `long_url` is an absolute `http` or `https` URL.
4. If `custom_code` is provided:
   - Validates format (`[a-zA-Z0-9_-]`, 3–32 chars).
   - Inserts into Postgres.
   - If the code already exists → `409 Conflict`.
5. If `custom_code` is omitted:
   - Generates up to 5 random 7-character base62 codes.
   - Tries to insert; retries on collision.
6. Writes to Redis: `url:{short_code} → long_url` (configurable TTL).
7. Returns `201` with the created object.

### 2. Redirect (`GET /:code`) — hot path

Built for high traffic:

1. Rate limit by client IP.
2. **Cache-first**: `GET` Redis key `url:{code}`.
3. **Hit** → respond `302 Location: <long_url>` without touching Postgres.
4. **Miss** → query Postgres:
   - If missing → `404`.
   - If found → `SETEX` in Redis, then `302`.
5. In parallel (goroutine), increment `click_count` in Postgres.
   The counter **does not block** the redirect response.

```
Request GET /docs
    │
    ▼
Rate limit (IP) ──exceeded──▶ 429
    │ ok
    ▼
Redis GET url:docs
    │
    ├── HIT ──────────────────────────▶ 302 + async click
    │
    └── MISS ▶ Postgres
                  │
                  ├── not found ▶ 404
                  └── found ▶ Redis SETEX ▶ 302 + async click
```

### 3. Get details (`GET /api/v1/urls/:code`)

Returns the full record from Postgres, including the current `click_count`. Requires API key.

**Note:** this endpoint does **not** increment clicks. Only the redirect (`GET /:code`) does.

### 4. Update (`PUT /api/v1/urls/:code`)

1. You can change `long_url`, `custom_code`, or both.
2. Updates Postgres.
3. Cache invalidation:
   - If the code changes: `DEL url:{old_code}` and `SET url:{new_code}`.
   - If only the URL changes: `SET url:{code}` with the new destination.
4. Returns `200` with the updated record.

### 5. Delete (`DELETE /api/v1/urls/:code`)

1. Deletes the row in Postgres.
2. Immediate `DEL` in Redis.
3. Returns `204 No Content`.
4. Later redirects for that code return `404`.

---

## Cache (Redis)

| Aspect | Detail |
|--------|--------|
| Key | `url:{short_code}` |
| Value | string with the `long_url` |
| TTL | `CACHE_TTL` (default `24h`) |
| Policy | Redis is **always** checked before Postgres on redirects |
| Create | `SET` after insert |
| Update | `DEL` old code if it changed + `SET` current |
| Delete | Immediate `DEL` |
| Redis get failure | Logs a warning and falls back to Postgres |

Goal: most redirects resolve from Redis alone, keeping load off the database.

---

## Rate limiting

Implementation: fixed window with Redis (`INCR` + `EXPIRE`).

| Route | Key | Default limit |
|-------|-----|---------------|
| `GET /:code` | Client IP | `120` requests / `1m` |
| `/api/v1/*` | `X-API-Key` value | `60` requests / `1m` |

When exceeded:

- Status: `429 Too Many Requests`
- Body: `{"error":"rate limit exceeded"}`
- Header: `Retry-After` (window seconds)

---

## Authentication

- Required header on `/api/v1/*`: `X-API-Key`
- Valid keys come from `API_KEYS` (comma-separated list)
- Constant-time comparison (`crypto/subtle`)
- Missing or invalid key → `401 Unauthorized`
- Redirect (`GET /:code`) and `/health` are public (no API key)

---

## Full API

Local base URL: `http://localhost:8080`

### Public

#### `GET /health`

Checks that the server is up.

```json
{ "status": "ok" }
```

#### `GET /:code`

Redirects to the long URL.

- `302 Found` + `Location` header
- `404` if the code does not exist
- `429` if the rate limit is exceeded

---

### Protected (`X-API-Key` required)

#### `POST /api/v1/urls`

Creates a short URL.

**Body:**

```json
{
  "long_url": "https://example.com/very/long/path",
  "custom_code": "docs"
}
```

`custom_code` is optional. If omitted, the server generates a 7-character code.

**Response `201`:**

```json
{
  "id": 1,
  "short_code": "docs",
  "long_url": "https://example.com/very/long/path",
  "click_count": 0,
  "created_at": "2026-09-27T16:00:00Z",
  "updated_at": "2026-09-27T16:00:00Z"
}
```

#### `GET /api/v1/urls/:code`

Returns URL details (including click count).

**Response `200`:** same schema as above.

#### `PUT /api/v1/urls/:code`

Updates the long URL and/or short code. At least one field is required.

```json
{
  "long_url": "https://example.com/updated",
  "custom_code": "docs2"
}
```

**Response `200`:** updated object.

#### `DELETE /api/v1/urls/:code`

Deletes the URL.

**Response:** `204 No Content` (empty body).

---

## Validation

| Field | Rule |
|-------|------|
| `long_url` | Required; absolute URL with `http` or `https` scheme and a host |
| `custom_code` / `:code` | Only `[a-zA-Z0-9_-]`, length 3–32 |
| Unique code | If already taken → `409` |

---

## Error codes

| Status | When |
|--------|------|
| `400` | Invalid body, invalid URL, or bad short-code format |
| `401` | Missing or invalid API key |
| `404` | Code not found |
| `409` | Short code already in use |
| `429` | Rate limit exceeded |
| `500` | Internal error (e.g. could not generate a unique code) |

Typical error body:

```json
{ "error": "descriptive message" }
```

---

## Local setup

### Requirements

- Go 1.22+
- Docker / Docker Compose

### Steps

```bash
# 1. Start Postgres + Redis
docker compose up -d

# 2. Configure environment
cp .env.example .env
# Edit API_KEYS and other values if needed

# 3. Download dependencies
go mod tidy

# 4. Start the server
go run ./cmd/server
```

Server listens on `http://localhost:8080`.

Stop infrastructure:

```bash
docker compose down
```

---

## Environment variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVER_ADDR` | HTTP listen address | `:8080` |
| `DATABASE_URL` | PostgreSQL DSN | (required) |
| `REDIS_ADDR` | Redis host:port | `localhost:6379` |
| `REDIS_PASSWORD` | Redis password | empty |
| `REDIS_DB` | Redis DB index | `0` |
| `CACHE_TTL` | Redirect cache TTL | `24h` |
| `API_KEYS` | Valid keys, comma-separated | (required) |
| `RATE_LIMIT_REDIRECT` | Max redirects per IP / window | `120` |
| `RATE_LIMIT_API` | Max API ops per key / window | `60` |
| `RATE_LIMIT_WINDOW` | Rate-limit window duration | `1m` |

Example `DATABASE_URL`:

```
postgres://urlshortener:urlshortener@localhost:5432/urlshortener?sslmode=disable
```

---

## Postman test order

Base: `http://localhost:8080`  
Header for `/api/v1/*` routes: `X-API-Key: dev-api-key-change-me`

| # | Method | Path | Notes |
|---|--------|------|-------|
| 1 | `GET` | `/health` | No API key → `{"status":"ok"}` |
| 2 | `POST` | `/api/v1/urls` | Body with `long_url` (no custom) → save `short_code` |
| 3 | `POST` | `/api/v1/urls` | Body with `custom_code: "docs"` |
| 4 | `GET` | `/api/v1/urls/docs` | Details; `click_count` should be `0` |
| 5 | `GET` | `/docs` | Redirect `302` (disable Follow redirects in Postman) |
| 6 | `GET` | `/api/v1/urls/docs` | `click_count` ≥ 1 |
| 7 | `PUT` | `/api/v1/urls/docs` | Change `long_url` and/or `custom_code` |
| 8 | `GET` | `/docs` (or new code) | Confirm new destination |
| 9 | `DELETE` | `/api/v1/urls/docs` | Expect `204` |
| 10 | `GET` | `/api/v1/urls/docs` and `/docs` | Both should return `404` |

Extra cases: no API key (`401`), duplicate code (`409`), invalid URL (`400`), redirect spam (`429`).

### Update examples

Change destination only:

```json
{ "long_url": "https://example.com/new" }
```

Rename code only:

```json
{ "custom_code": "docs2" }
```

After renaming to `docs2`, use `GET /docs2` for redirects. `GET /docs` correctly returns `404`.

---

## curl examples

```bash
# Health
curl -s http://localhost:8080/health

# Create with generated code
curl -s -X POST http://localhost:8080/api/v1/urls \
  -H "Content-Type: application/json" \
  -H "X-API-Key: dev-api-key-change-me" \
  -d '{"long_url":"https://example.com/very/long/path"}'

# Create with custom code
curl -s -X POST http://localhost:8080/api/v1/urls \
  -H "Content-Type: application/json" \
  -H "X-API-Key: dev-api-key-change-me" \
  -d '{"long_url":"https://example.com","custom_code":"docs"}'

# Details
curl -s http://localhost:8080/api/v1/urls/docs \
  -H "X-API-Key: dev-api-key-change-me"

# Update
curl -s -X PUT http://localhost:8080/api/v1/urls/docs \
  -H "Content-Type: application/json" \
  -H "X-API-Key: dev-api-key-change-me" \
  -d '{"long_url":"https://example.com/updated"}'

# Redirect (inspect 302 headers)
curl -i http://localhost:8080/docs

# Delete
curl -i -X DELETE http://localhost:8080/api/v1/urls/docs \
  -H "X-API-Key: dev-api-key-change-me"
```

---

## Tests

```bash
# Unit tests (always runnable; Redis covered via miniredis)
go test ./...

# With live Postgres + Redis integration tests
export DATABASE_URL='postgres://urlshortener:urlshortener@localhost:5432/urlshortener?sslmode=disable'
export REDIS_ADDR='localhost:6379'
go test ./...

# Coverage summary
go test ./... -cover
```

Integration notes:

- `internal/database` and `internal/repository` tests use `TEST_DATABASE_URL` or `DATABASE_URL` (skip if unset/unreachable).
- Live Redis checks use `TEST_REDIS_ADDR` or `REDIS_ADDR` (skip if unreachable).
- Cache/rate-limit unit tests use in-memory [miniredis](https://github.com/alicebob/miniredis) and do not need a real Redis instance.

Main coverage:

- `config`: load defaults, custom values, missing/invalid env
- `shortcode`: generate length, alphabet, uniqueness
- `cache`: get/set/delete, TTL expiry, rate limiter allow/deny/reset
- `database`: Postgres/Redis connect success and failure
- `repository`: CRUD, conflicts, not found (Postgres)
- `service`: create/get/update/delete/redirect, validation, cache-first, fallback
- `handler`: all HTTP handlers and error mappings
- `middleware`: API key and rate limit middleware
- `server`: router wiring for health, API auth, and redirect

---

## Out of scope (v1)

- Frontend / UI
- Multi-user JWT auth
- Advanced analytics (geo, user-agent, etc.)
- Automatic URL expiration
- Admin panel
