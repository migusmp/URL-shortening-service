# URL Shortener Service

Servicio backend para acortar URLs largas en URLs cortas, construido en **Go** con el framework **Gin**.

Permite crear, consultar, actualizar y eliminar URLs cortas (con código generado o personalizado), redirigir con alto rendimiento mediante **Redis** (cache-first), persistir en **PostgreSQL** y proteger la API de escritura con **API key** y **rate limiting**.

---

## Índice

1. [Qué hace el sistema](#qué-hace-el-sistema)
2. [Arquitectura](#arquitectura)
3. [Stack tecnológico](#stack-tecnológico)
4. [Estructura del proyecto](#estructura-del-proyecto)
5. [Modelo de datos](#modelo-de-datos)
6. [Flujos de funcionamiento](#flujos-de-funcionamiento)
7. [Cache (Redis)](#cache-redis)
8. [Rate limiting](#rate-limiting)
9. [Autenticación](#autenticación)
10. [API completa](#api-completa)
11. [Validaciones](#validaciones)
12. [Códigos de error](#códigos-de-error)
13. [Arranque local](#arranque-local)
14. [Variables de entorno](#variables-de-entorno)
15. [Orden de prueba en Postman](#orden-de-prueba-en-postman)
16. [Ejemplos curl](#ejemplos-curl)
17. [Tests](#tests)

---

## Qué hace el sistema

| Capacidad | Descripción |
|-----------|-------------|
| Acortar URLs | Transforma una URL larga en un código corto (generado o custom) |
| Redirect | `GET /:code` responde `302` hacia la URL original |
| Personalización | El usuario puede elegir su propio `custom_code` |
| Persistencia | Guarda `short_code`, `long_url` y `click_count` en PostgreSQL |
| Cache | Antes de consultar la BD, mira Redis |
| Contador de visitas | Cada redirect incrementa `click_count` (de forma asíncrona) |
| CRUD | Crear, leer, actualizar y eliminar URLs cortas |
| Seguridad de escritura | Las rutas de gestión exigen header `X-API-Key` |
| Rate limiting | Si se supera el límite de requests, responde `429` |

---

## Arquitectura

```
┌──────────┐     ┌─────────────────────────────────────────────┐
│ Cliente  │────▶│                   Gin                        │
└──────────┘     │  ┌─────────────┐  ┌──────────────────────┐  │
                 │  │ Rate Limit  │  │ API Key (solo /api)  │  │
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

Capas internas:

| Capa | Responsabilidad |
|------|-----------------|
| `handler` | HTTP: parseo JSON, status codes, redirects |
| `middleware` | API key y rate limit |
| `service` | Reglas de negocio, validación, orquestación cache/DB |
| `repository` | Acceso a PostgreSQL (pgx) |
| `cache` | Acceso a Redis (get/set/del + contadores de rate limit) |
| `shortcode` | Generación aleatoria base62 |
| `domain` | Entidad `URL` y errores de dominio |
| `config` | Carga de variables de entorno |

---

## Stack tecnológico

- **Go 1.22+** — lenguaje
- **Gin** — HTTP router/framework
- **PostgreSQL 16** — fuente de verdad
- **Redis 7** — cache de redirects + rate limiting
- **pgx** — driver PostgreSQL
- **go-redis** — cliente Redis
- **godotenv** — carga de `.env` en local
- **Docker Compose** — Postgres + Redis

---

## Estructura del proyecto

```
url_shortener/
├── cmd/server/main.go              # Entrypoint: wiring, HTTP server, graceful shutdown
├── internal/
│   ├── config/                     # Configuración desde env
│   ├── domain/                     # Entidad URL + errores
│   ├── handler/                    # Handlers Gin
│   ├── middleware/                 # APIKeyAuth + RateLimit
│   ├── repository/                 # CRUD PostgreSQL
│   ├── cache/                      # Redis URL cache + rate limiter
│   ├── service/                    # Lógica de negocio
│   └── shortcode/                  # Generador base62 (7 chars)
├── migrations/
│   └── 001_create_urls.sql         # Schema inicial (se aplica al subir Postgres)
├── docker-compose.yml
├── .env.example
├── go.mod
└── README.md
```

---

## Modelo de datos

Tabla `urls` en PostgreSQL:

| Columna | Tipo | Descripción |
|---------|------|-------------|
| `id` | `BIGSERIAL` | Clave primaria |
| `short_code` | `VARCHAR(32)` | Código corto único (índice UNIQUE) |
| `long_url` | `TEXT` | URL original de destino |
| `click_count` | `BIGINT` | Número de veces que se ha hecho redirect |
| `created_at` | `TIMESTAMPTZ` | Fecha de creación |
| `updated_at` | `TIMESTAMPTZ` | Última modificación |

La migración se ejecuta automáticamente al levantar Postgres con Docker Compose (`/docker-entrypoint-initdb.d`).

---

## Flujos de funcionamiento

### 1. Crear URL corta (`POST /api/v1/urls`)

1. Middleware valida `X-API-Key`.
2. Middleware aplica rate limit por API key.
3. Se valida que `long_url` sea `http` o `https` absoluta.
4. Si viene `custom_code`:
   - Se valida formato (`[a-zA-Z0-9_-]`, 3–32 chars).
   - Se inserta en Postgres.
   - Si el código ya existe → `409 Conflict`.
5. Si no viene `custom_code`:
   - Se generan hasta 5 códigos aleatorios base62 de 7 caracteres.
   - Se intenta insertar; ante colisión se reintenta.
6. Se escribe en Redis: `url:{short_code} → long_url` (TTL configurable).
7. Respuesta `201` con el objeto creado.

### 2. Redirect (`GET /:code`) — path crítico

Diseñado para soportar mucho tráfico:

1. Rate limit por IP del cliente.
2. **Cache-first**: `GET` en Redis clave `url:{code}`.
3. **Hit** → responde `302 Location: <long_url>` sin tocar Postgres.
4. **Miss** → consulta Postgres:
   - Si no existe → `404`.
   - Si existe → `SETEX` en Redis y luego `302`.
5. En paralelo (goroutine), se incrementa `click_count` en Postgres.
   El contador **no bloquea** la respuesta del redirect.

```
Request GET /docs
    │
    ▼
Rate limit (IP) ──excede──▶ 429
    │ ok
    ▼
Redis GET url:docs
    │
    ├── HIT ──────────────────────────▶ 302 + click async
    │
    └── MISS ▶ Postgres
                  │
                  ├── no existe ▶ 404
                  └── existe ▶ Redis SETEX ▶ 302 + click async
```

### 3. Consultar detalle (`GET /api/v1/urls/:code`)

Devuelve el registro completo desde Postgres, incluyendo `click_count` actualizado. Requiere API key.

### 4. Actualizar (`PUT /api/v1/urls/:code`)

1. Se puede cambiar `long_url`, `custom_code`, o ambos.
2. Se actualiza en Postgres.
3. Invalidación de cache:
   - Si cambia el código: `DEL url:{codigo_viejo}` y `SET url:{codigo_nuevo}`.
   - Si solo cambia la URL: `SET url:{codigo}` con el nuevo destino.
4. Respuesta `200` con el registro actualizado.

### 5. Eliminar (`DELETE /api/v1/urls/:code`)

1. Borra la fila en Postgres.
2. `DEL` inmediato en Redis.
3. Respuesta `204 No Content`.
4. Los siguientes redirects a ese código devolverán `404`.

---

## Cache (Redis)

| Aspecto | Detalle |
|---------|---------|
| Clave | `url:{short_code}` |
| Valor | string con la `long_url` |
| TTL | `CACHE_TTL` (por defecto `24h`) |
| Política | **Siempre** se consulta Redis antes que Postgres en redirects |
| Create | `SET` tras insertar |
| Update | `DEL` del código antiguo si cambia + `SET` del actual |
| Delete | `DEL` inmediato |
| Fallo Redis en get | Se registra warning y se hace fallback a Postgres |

El objetivo es que la mayoría de redirects se resuelvan solo con Redis, sin presión sobre la base de datos.

---

## Rate limiting

Implementación: ventana fija con Redis (`INCR` + `EXPIRE`).

| Ruta | Clave | Límite por defecto |
|------|-------|--------------------|
| `GET /:code` | IP del cliente | `120` requests / `1m` |
| `/api/v1/*` | valor de `X-API-Key` | `60` requests / `1m` |

Si se excede:

- Status: `429 Too Many Requests`
- Body: `{"error":"rate limit exceeded"}`
- Header: `Retry-After` (segundos de la ventana)

---

## Autenticación

- Header obligatorio en `/api/v1/*`: `X-API-Key`
- Las keys válidas se definen en `API_KEYS` (lista separada por comas)
- Comparación en tiempo constante (`crypto/subtle`)
- Sin key o key inválida → `401 Unauthorized`
- El redirect (`GET /:code`) y `/health` son públicos (sin API key)

---

## API completa

Base URL local: `http://localhost:8080`

### Públicas

#### `GET /health`

Comprueba que el servidor responde.

```json
{ "status": "ok" }
```

#### `GET /:code`

Redirige a la URL larga.

- `302 Found` + header `Location`
- `404` si el código no existe
- `429` si se supera el rate limit

---

### Protegidas (`X-API-Key` obligatorio)

#### `POST /api/v1/urls`

Crea una URL corta.

**Body:**

```json
{
  "long_url": "https://example.com/very/long/path",
  "custom_code": "docs"
}
```

`custom_code` es opcional. Si se omite, el servidor genera uno de 7 caracteres.

**Respuesta `201`:**

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

Devuelve el detalle de una URL (incluye contador de clicks).

**Respuesta `200`:** mismo esquema que arriba.

#### `PUT /api/v1/urls/:code`

Actualiza la URL y/o el código corto. Hay que enviar al menos uno de los campos.

```json
{
  "long_url": "https://example.com/updated",
  "custom_code": "docs2"
}
```

**Respuesta `200`:** objeto actualizado.

#### `DELETE /api/v1/urls/:code`

Elimina la URL.

**Respuesta:** `204 No Content` (sin body).

---

## Validaciones

| Campo | Regla |
|-------|--------|
| `long_url` | Obligatoria; URL absoluta con esquema `http` o `https` y host |
| `custom_code` / `:code` | Solo `[a-zA-Z0-9_-]`, longitud 3–32 |
| Código único | Si ya existe → `409` |

---

## Códigos de error

| Status | Cuándo |
|--------|--------|
| `400` | Body inválido, URL inválida o código con formato incorrecto |
| `401` | Falta o es inválida la API key |
| `404` | Código no encontrado |
| `409` | Código corto ya en uso |
| `429` | Rate limit excedido |
| `500` | Error interno (p. ej. no se pudo generar un código único) |

Formato típico del body de error:

```json
{ "error": "mensaje descriptivo" }
```

---

## Arranque local

### Requisitos

- Go 1.22+
- Docker / Docker Compose

### Pasos

```bash
# 1. Levantar Postgres + Redis
docker compose up -d

# 2. Configurar entorno
cp .env.example .env
# Edita API_KEYS y demás si lo necesitas

# 3. Descargar dependencias
go mod tidy

# 4. Arrancar el servidor
go run ./cmd/server
```

El servidor queda en `http://localhost:8080`.

Para detener la infra:

```bash
docker compose down
```

---

## Variables de entorno

| Variable | Descripción | Default |
|----------|-------------|---------|
| `SERVER_ADDR` | Dirección de escucha HTTP | `:8080` |
| `DATABASE_URL` | DSN de PostgreSQL | (requerido) |
| `REDIS_ADDR` | Host:puerto de Redis | `localhost:6379` |
| `REDIS_PASSWORD` | Password de Redis | vacío |
| `REDIS_DB` | Índice de base Redis | `0` |
| `CACHE_TTL` | TTL del cache de redirects | `24h` |
| `API_KEYS` | Keys válidas separadas por coma | (requerido) |
| `RATE_LIMIT_REDIRECT` | Máx. redirects por IP / ventana | `120` |
| `RATE_LIMIT_API` | Máx. ops API por key / ventana | `60` |
| `RATE_LIMIT_WINDOW` | Duración de la ventana | `1m` |

Ejemplo de `DATABASE_URL`:

```
postgres://urlshortener:urlshortener@localhost:5432/urlshortener?sslmode=disable
```

---

## Orden de prueba en Postman

Base: `http://localhost:8080`  
Header para rutas `/api/v1/*`: `X-API-Key: dev-api-key-change-me`

| # | Método | Ruta | Notas |
|---|--------|------|-------|
| 1 | `GET` | `/health` | Sin API key → `{"status":"ok"}` |
| 2 | `POST` | `/api/v1/urls` | Body con `long_url` (sin custom) → guarda `short_code` |
| 3 | `POST` | `/api/v1/urls` | Body con `custom_code: "docs"` |
| 4 | `GET` | `/api/v1/urls/docs` | Detalle; `click_count` debería ser `0` |
| 5 | `GET` | `/docs` | Redirect `302` (desactiva Follow redirects en Postman) |
| 6 | `GET` | `/api/v1/urls/docs` | `click_count` ≥ 1 |
| 7 | `PUT` | `/api/v1/urls/docs` | Cambia `long_url` y/o `custom_code` |
| 8 | `GET` | `/docs` (o nuevo código) | Comprueba el nuevo destino |
| 9 | `DELETE` | `/api/v1/urls/docs` | Esperado `204` |
| 10 | `GET` | `/api/v1/urls/docs` y `/docs` | Ambos deben dar `404` |

Casos extra: sin API key (`401`), código duplicado (`409`), URL inválida (`400`), spam de redirects (`429`).

---

## Ejemplos curl

```bash
# Health
curl -s http://localhost:8080/health

# Crear con código generado
curl -s -X POST http://localhost:8080/api/v1/urls \
  -H "Content-Type: application/json" \
  -H "X-API-Key: dev-api-key-change-me" \
  -d '{"long_url":"https://example.com/very/long/path"}'

# Crear con código personalizado
curl -s -X POST http://localhost:8080/api/v1/urls \
  -H "Content-Type: application/json" \
  -H "X-API-Key: dev-api-key-change-me" \
  -d '{"long_url":"https://example.com","custom_code":"docs"}'

# Detalle
curl -s http://localhost:8080/api/v1/urls/docs \
  -H "X-API-Key: dev-api-key-change-me"

# Actualizar
curl -s -X PUT http://localhost:8080/api/v1/urls/docs \
  -H "Content-Type: application/json" \
  -H "X-API-Key: dev-api-key-change-me" \
  -d '{"long_url":"https://example.com/updated"}'

# Redirect (ver headers del 302)
curl -i http://localhost:8080/docs

# Eliminar
curl -i -X DELETE http://localhost:8080/api/v1/urls/docs \
  -H "X-API-Key: dev-api-key-change-me"
```

---

## Tests

```bash
go test ./...
```

Cobertura principal:

- Service: creación (custom y generada), conflictos, cache-first en redirect, fallback a DB, invalidación en update/delete
- Middleware: API key presente / inválida / válida
- Rate limit: permite dentro del límite y deniega al excederlo

---

## Fuera de scope (v1)

- Frontend / UI
- Multi-usuario con JWT
- Analytics avanzadas (geo, user-agent, etc.)
- Expiración automática de URLs
- Panel de administración
