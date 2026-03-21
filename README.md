# isdict-api

English | [中文](README.zh-CN.md)

[![Go Version](https://img.shields.io/badge/go-1.25-blue.svg)](https://golang.org)
[![Go Report Card](https://goreportcard.com/badge/github.com/simp-lee/isdict-api)](https://goreportcard.com/report/github.com/simp-lee/isdict-api)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Test](https://github.com/simp-lee/isdict-api/workflows/Test/badge.svg)](https://github.com/simp-lee/isdict-api/actions)

isdict-api is a Gin-based English-Chinese dictionary service backed by PostgreSQL. It serves a REST API under `/api/v1`, exposes liveness and readiness probes, and includes a built-in web UI for local lookup.

Detailed API fields and examples live in [api.md](api.md).

## Key Features

IsDict integrates six authoritative dictionary data sources (Wiktionary, Oxford, CEFR-J, CET, ECDICT, WordFreq), offering:

- **Comprehensive Dictionary Data**: 715,000+ entries with pronunciations, definitions, examples, and word forms
- **Multi-Accent Support**: IPA phonetics for 10 accents (British, American, Australian, Canadian, New Zealand, etc.), 160,000+ pronunciation records
- **Bilingual Support**: 976,000 English senses + 260,000 Chinese translations, 584,000 authentic bilingual example sentences
- **Powerful Search & Query**: Prefix autocomplete, intelligent phrase matching, 888,000 word form variants lookup
- **Multi-Dimensional Level Tags**: CEFR A1-C2, CET-4/6, Oxford 3000/5000, Collins stars, word frequency TOP 50K
- **Production-Ready Middleware**: Rate limiting, CORS, caching, request ID tracking, and timeouts; see [Middleware Notes](#middleware-notes)
- **Out-of-the-Box**: Health checks, graceful shutdown, connection pooling, and request limits
- **Built-in Web Console**: Lightweight Alpine.js/Tailwind single-page app for quick lookups

## What It Includes

- Dictionary endpoints for words, variants, pronunciations, senses, search, suggest, and phrase lookup
- PostgreSQL-backed storage with required extension checks for `pg_trgm`
- Middleware for rate limiting, CORS, request timeout, request ID, caching, recovery, and logging
- Local static UI served at `/` and bundled JavaScript at `/static/js`
- Migration CLI in `cmd/migrate-db`

## Requirements

- Go 1.25+
- PostgreSQL 14+
- Node.js 22+ only if you run the web regression test or `make test`

## Quick Start

```bash
git clone https://github.com/simp-lee/isdict-api.git
cd isdict-api
go mod download
cp configs/api.example.env configs/api.env
# Edit configs/api.env
make db-setup
make run
```

By default, `make run` and `make db-setup` load `configs/api.env` through `ISDICT_API_ENV_FILE`.

Default local URLs:

- API base: http://localhost:8080/api/v1
- Web UI: http://localhost:8080/
- Liveness: http://localhost:8080/health
- Readiness: http://localhost:8080/api/v1/health

## Configuration

The application reads process environment first.

- If `ISDICT_API_ENV_FILE` is set, only that file is loaded.
- Otherwise it tries `.env`, `api/.env`, then `../.env`.
- `DB_*` values also accept `PG*` aliases such as `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGDATABASE`, and `PGSSLMODE`.

Common settings:

| Variable | Default | Notes |
|----------|---------|-------|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | PostgreSQL user |
| `DB_PASSWORD` | `postgres` | PostgreSQL password |
| `DB_NAME` | `isdict` | Database name |
| `DB_SSLMODE` | `prefer` | Use `require` or stricter for managed PostgreSQL |
| `PORT` | `8080` | HTTP listen port |
| `GIN_MODE` | `debug` | `debug`, `release`, `test` |
| `DB_MAX_IDLE_CONNS` | `10` | Connection pool idle cap |
| `DB_MAX_OPEN_CONNS` | `100` | Connection pool max open |
| `API_BATCH_MAX_SIZE` | `100` | Max words in `POST /api/v1/words/batch` |
| `API_SEARCH_MAX_LIMIT` | `100` | Max `limit` for `/search` |
| `API_SUGGEST_MAX_LIMIT` | `50` | Max `limit` for `/suggest` |
| `ENABLE_RATE_LIMIT` | `true` | Rate limiting middleware |
| `ENABLE_CORS` | `true` | CORS middleware |
| `ENABLE_TIMEOUT` | `true` | Timeout middleware |
| `TIMEOUT_SECONDS` | `30` | Request timeout |
| `ENABLE_REQUEST_ID` | `true` | Adds `X-Request-Id` |
| `ENABLE_CACHE` | `true` | GET cache for `/api/*` |
| `ISDICT_API_ENV_FILE` | empty | Explicit env file path |

Readiness checks both database connectivity and required PostgreSQL extension availability. In practice, `/api/v1/health` returns `200` only when the database is reachable and `pg_trgm` is present.

## Database Workflow

The primary database workflow is the migration CLI in `cmd/migrate-db`.

```bash
# Apply migrations
ISDICT_API_ENV_FILE=configs/api.env go run ./cmd/migrate-db

# Verify migration-managed objects
ISDICT_API_ENV_FILE=configs/api.env go run ./cmd/migrate-db --verify

# Drop and recreate tables, guarded by explicit confirmation
ISDICT_API_ENV_FILE=configs/api.env \
go run ./cmd/migrate-db --drop --force --confirm-drop postgres@db.example.com:5432/isdict
```

Helper targets:

- `make db-setup`: run the migration CLI
- `make db-verify`: verify migration-managed objects through the migration CLI
- `make db-reset`: guarded reset flow; requires `CONFIRM_DROP` to exactly match the target database
- `make db-fixtures`: for an empty target database, rebuild migration-managed tables through the authoritative migration CLI and then load `db/sample_data.sql`; requires `CONFIRM_FIXTURES`

All four database helper targets resolve connection settings in the same order: exported `DB_*`, then exported `PG*`, then the file pointed to by `ISDICT_API_ENV_FILE` (default `configs/api.env` in the Makefile).

## HTTP Surface

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/health` | Liveness probe |
| `GET` | `/api/v1/health` | Readiness probe |
| `GET` | `/api/v1/words/:headword` | Full word entry |
| `GET` | `/api/v1/words/:headword/pronunciations` | Pronunciations only |
| `GET` | `/api/v1/words/:headword/senses` | Senses only |
| `GET` | `/api/v1/words/by-variant/:variant` | Reverse lookup by variant |
| `POST` | `/api/v1/words/batch` | Batch lookup |
| `GET` | `/api/v1/search` | Search with filters |
| `GET` | `/api/v1/suggest` | Autocomplete |
| `GET` | `/api/v1/phrases` | Phrase lookup |

Current request rules worth knowing:

- `cefr_level` accepts only `A1`, `A2`, `B1`, `B2`, `C1`, `C2`
- `/search` requires `q`; default `limit` is `20`
- `/suggest` requires `prefix`; default `limit` is `10`
- `/phrases` requires `q`; max `limit` is `50`
- `/health` and `/api/v1/health` return plain JSON, not the standard response envelope

Current response fields worth knowing:

- Word-like payloads returned by `/api/v1/words`, `/api/v1/words/by-variant`, `/api/v1/words/batch`, `/api/v1/search`, `/api/v1/suggest`, and `/api/v1/phrases` include the shared upstream annotation block
- That annotation block now includes `school_level` with numeric codes `0=unknown`, `1=junior middle school`, `2=senior high school`, `3=university`
- The API returns `school_level` as an integer passthrough so clients can choose their own labels or badge styles

## Middleware Notes

- `/health` and `/api/v1/health` bypass rate limiting and response caching
- Timeout middleware applies to normal API routes; readiness uses its own database ping timeout
- The built-in UI is served locally by the API process at `/`, with bundled JavaScript exposed at `/static/js`

## Tests

```bash
# Full default test flow
make test

# Go tests only
go test -v -race ./...

# Web regression test only
node --test web/dictionary_app.test.mjs

# Middleware package tests
go test ./internal/api/middleware -count=1

# Middleware probe clients
go run ./tests/middleware/basic
go run ./tests/middleware/verify
go run ./tests/middleware/stress
```

`make test` and `make test-coverage` now fail closed unless a disposable real PostgreSQL DSN is available. They honor exported `TEST_POSTGRES_DSN` and `TEST_POSTGRES_ADMIN_DSN` first, then derive both values from `DB_*`, `PG*`, or the file pointed to by `ISDICT_API_ENV_FILE` (default `configs/api.env`). The effective `TEST_POSTGRES_ADMIN_DSN` must connect as a role that has `CREATEDB`, because the real PostgreSQL suites create disposable databases through upstream helpers. Restricted-role migration fixtures still attempt `CREATEROLE` only inside their dedicated test path and skip that subset when the admin DSN lacks role-creation privileges.

The middleware probe clients assume `http://localhost:8080` unless you override them with environment variables.

PostgreSQL integration tests are guarded for disposable local or socket-based instances by default. To intentionally run them against a disposable non-local PostgreSQL instance, set `ISDICT_ALLOW_NONLOCAL_TEST_POSTGRES=true` alongside `TEST_POSTGRES_DSN` and, for migration tests that create databases or roles, `TEST_POSTGRES_ADMIN_DSN`.

## Project Layout

```text
cmd/            application entrypoints
configs/        environment templates
db/             sample fixture data
internal/       api handlers, middleware, app logging, config
tests/          middleware probe programs and tests
web/            built-in static UI
```

## License

MIT. See [LICENSE](LICENSE).
