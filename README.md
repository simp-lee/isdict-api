# isdict-api

English | [中文](README.zh-CN.md)

[![Go Version](https://img.shields.io/badge/go-1.24-blue.svg)](https://golang.org)
[![Go Report Card](https://goreportcard.com/badge/github.com/simp-lee/isdict-api)](https://goreportcard.com/report/github.com/simp-lee/isdict-api)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Test](https://github.com/simp-lee/isdict-api/workflows/Test/badge.svg)](https://github.com/simp-lee/isdict-api/actions)

English-Chinese dictionary API backed by PostgreSQL and the shared [isdict-commons](https://github.com/simp-lee/isdict-commons) models.

## Overview

- Consolidates Wiktionary, CEFR, Oxford, CET, ECDICT, and WordFreq datasets into a single API
- Returns pronunciations, bilingual definitions, variants, frequency stats, and learning tags
- Ships with a lightweight Alpine.js/Tailwind web console for quick lookups
- Production-ready middleware (rate limiting, CORS, caching, request ID tracking, and timeouts)—see [Middleware Features](#middleware-features) for details
- Provides health checks, graceful shutdown, connection pooling, and request limits out of the box

See [api.md](api.md) for the full API surface.

## Getting Started

### Prerequisites

- Go 1.24+
- PostgreSQL 14+
- Dictionary data (sample SQL or full dump)

### Run Locally

```bash
git clone https://github.com/simp-lee/isdict-api.git
cd isdict-api
go mod download
cp configs/api.example.env configs/api.env
# Edit configs/api.env with your database credentials
go run cmd/api/main.go
```

- REST API: http://localhost:8080/api/v1
- Web UI: http://localhost:8080
- Health check: http://localhost:8080/health

### Docker

```bash
docker build -t isdict-api .
docker run -d -p 8080:8080 --name isdict-api \
  -e DB_HOST=your-db-host \
  -e DB_USER=your-user \
  -e DB_PASSWORD=your-password \
  isdict-api
```

Or use Docker Compose for a complete local environment with PostgreSQL:

```bash
docker-compose up -d
```

## Configuration

Runtime settings are loaded from environment variables and a `.env` file (if present). The loader checks, in order: environment variables → `configs/api.env` → `.env` → `api/.env` → `../.env`, stopping at the first file it finds.

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | localhost | PostgreSQL host |
| `DB_PORT` | 5432 | PostgreSQL port |
| `DB_USER` | postgres | Database user |
| `DB_PASSWORD` | postgres | Database password |
| `DB_NAME` | isdict | Database name |
| `DB_SSLMODE` | prefer | PostgreSQL SSL mode |
| `PORT` | 8080 | HTTP listen port |
| `GIN_MODE` | debug | `debug`, `release`, or `test` |
| `DB_MAX_IDLE_CONNS` | 10 | Connection pool idle cap |
| `DB_MAX_OPEN_CONNS` | 100 | Connection pool max open |
| `API_BATCH_MAX_SIZE` | 100 | Max words per batch request |
| `API_SEARCH_MAX_LIMIT` | 100 | Max search results |
| `API_SUGGEST_MAX_LIMIT` | 50 | Max suggestions |

### Middleware Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `ENABLE_RATE_LIMIT` | true | Enable rate limiting |
| `RATE_LIMIT_RPS` | 100 | Requests per second limit |
| `RATE_LIMIT_BURST` | 200 | Burst capacity for token bucket |
| `RATE_LIMIT_PER_HOUR` | 10000 | Requests per hour limit |
| `RATE_LIMIT_PER_DAY` | 0 | Requests per day limit (0=disabled) |
| `ENABLE_CORS` | true | Enable CORS headers |
| `CORS_ALLOW_ORIGINS` | * | Allowed origins (comma-separated) |
| `ENABLE_TIMEOUT` | true | Enable request timeout |
| `TIMEOUT_SECONDS` | 30 | Request timeout duration |
| `ENABLE_REQUEST_ID` | true | Enable request ID generation |
| `ENABLE_CACHE` | true | Enable response caching |
| `CACHE_MAX_SIZE` | 1000 | Maximum cache entries |
| `CACHE_EXPIRATION_MINS` | 5 | Cache entry expiration time |

**Notes:**
- Rate limiting is bypassed for `/health` and static files
- Response caching applies only to GET requests on `/api/*` paths
- Request IDs are included in all responses via `X-Request-Id` header
- Rate limit headers: `X-Ratelimit-Limit`, `X-Ratelimit-Limit-Hour`, `X-Ratelimit-Remaining`

## Database

The schema and indexes are managed via database migrations in `isdict-commons/migration`. Use the `cmd/migrate-db` tool for all database operations.

### Production Workflow

```bash
# Fresh migration (drop and recreate all tables)
go run cmd/migrate-db/main.go --drop

# Incremental migration (create missing tables/indexes)
go run cmd/migrate-db/main.go

# Verify migration status
go run cmd/migrate-db/main.go --verify
```

### Quick Test with Sample Data

For quick local testing, you can use the SQL helpers in the `db/` directory:

```bash
createdb isdict
psql -d isdict -f db/schema.sql
psql -d isdict -f db/indexes.sql
psql -d isdict -f db/sample_data.sql
```

**Note:** The SQL files in `db/` are for reference and testing only. They may lag behind the authoritative migrations in `isdict-commons/migration`.

## Middleware Features

The API includes production-ready middleware powered by [ginx](https://github.com/simp-lee/ginx):

- **Rate Limiting**: Token bucket algorithm with configurable RPS, burst, and hourly/daily limits
- **CORS**: Cross-origin resource sharing with configurable origins and methods
- **Response Caching**: Automatic GET request caching with configurable size and TTL
- **Request ID**: Unique request tracking for debugging and logging
- **Timeout Protection**: Configurable request timeout to prevent resource exhaustion
- **Panic Recovery**: Automatic recovery from panics with structured error responses
- **Structured Logging**: Request/response logging with timing and status codes

Run verification tests:
```bash
# Start the API server first
go run cmd/api/main.go

# In another terminal, run middleware tests
go run tests/middleware/verify/main.go

# Or run stress tests
go run tests/middleware/stress/main.go
```

## Web Console

- Single-page app served from `web/index.html`
- Instant suggestions with CEFR/Oxford/CET badges and frequency ranks
- Variants, bilingual definitions, pronunciations, and tagged examples in dedicated tabs
- Works out of the box at `http://localhost:8080`
- Configure `apiBaseURL` in `index.html` if pointing to a remote API endpoint

## Project Layout

```
cmd/
  api/          # API entrypoint
  migrate-db/   # Database migration tool
configs/        # Environment templates
db/             # SQL references (schema, indexes, sample data)
internal/       # Handlers, services, repositories, configuration
  api/          # API layer (handler, service, repository, middleware)
  config/       # Configuration loader
tests/          # Test suites
  middleware/   # Middleware tests (basic, verify, stress)
web/            # Static web console (Alpine.js + Tailwind)
```

## License

MIT License. See `LICENSE` for details.

Built for English learners and educators.
