# API Reference

English | [中文](api.zh-CN.md)

Comprehensive reference for the `isdict-api` service.

## Base URL & Versioning

- Default base URL during local development: `http://localhost:8080`
- Dictionary and business endpoints are rooted under `/api/v1`; operational endpoints such as `/health` are exposed outside that prefix
- The service is currently unauthenticated; apply your own gateway or proxy when running in production

## Response Envelope

Every API endpoint returns a unified response structure defined in `isdict-commons/model/response.go`:

```json
{
  "success": true,
  "data": {},
  "error": null,
  "meta": null
}
```

**Fields:**
- `success` (boolean): `true` for successful requests, `false` for errors
- `data` (any): The response payload (object, array, or null)
- `error` (object|null): Error details when `success` is `false`, `null` otherwise
  - `code` (string): Error code (see [Error Codes](#error-codes))
  - `message` (string): Human-readable error message
  - `details` (any): Optional additional error context
- `meta` (object|null): Optional metadata for pagination and statistics

The envelope remains part of the public contract for dictionary endpoints. Response payloads are not flattened for backward compatibility: word objects still use `headword` and `senses`, and pronunciation data remains a `pronunciations[]` array structure.

**Special case:** `/health` and `/api/v1/health` return plain JSON without the envelope for simplicity.

## Words

### GET /api/v1/words/{headword}

Return a complete dictionary entry for the supplied headword (case-insensitive).

| Query | Type | Default | Description |
|-------|------|---------|-------------|
| `accent` | string | — | Filter pronunciations by accent enum (see [Enums](#enumerations)) |
| `include_variants` | bool | `true` | Include morphological variants |
| `include_pronunciations` | bool | `true` | Include pronunciation list |
| `include_senses` | bool | `true` | Include definitions and examples |

```bash
curl "http://localhost:8080/api/v1/words/run?include_pronunciations=true&include_senses=true"
```

### GET /api/v1/words/{headword}/pronunciations

Return pronunciations only. Supports the same `accent` filter as the main endpoint.

### GET /api/v1/words/{headword}/senses

Return definitions/examples only.

| Query | Type | Default | Description |
|-------|------|---------|-------------|
| `pos` | string | — | Filter by part of speech |
| `lang` | string | `both` | `both`, `en`, or `zh` |

### GET /api/v1/words/by-variant/{variant}

Resolve a variant (e.g., "running") back to its canonical word(s).

| Query | Type | Default | Description |
|-------|------|---------|-------------|
| `kind` | string | — | Filter to `form` (inflected form) or `alias` |
| `include_pronunciations` | bool | `true` | Include pronunciations |
| `include_senses` | bool | `true` | Include senses |

### POST /api/v1/words/batch

Bulk lookup of up to `API_BATCH_MAX_SIZE` words in one call. Duplicate and blank entries are ignored server-side.

```json
{
  "words": ["hello", "world"],
  "include_variants": true,
  "include_pronunciations": true,
  "include_senses": true
}
```

The response `meta` section reports `requested`, `found`, and `not_found` lists.

## Search & Discovery

### GET /api/v1/search

Fuzzy search with ranking and filters. Requires `q` whose normalized form is at least 3 Unicode characters long.

| Query | Type | Default | Notes |
|-------|------|---------|-------|
| `q` | string | — | Search keyword (required; normalized minimum 3 Unicode characters after trimming, lowercasing, and removing spaces, hyphens, and underscores) |
| `pos` | string | — | Lowercase POS filter (see [Part of Speech](#part-of-speech)) |
| `cefr_level` | string | — | `A1`, `A2`, `B1`, `B2`, `C1`, or `C2` (case-insensitive) |
| `oxford_level` | int | — | 0 (any), 1 (Oxford 3000), 2 (Oxford 5000) |
| `cet_level` | int | — | 0 (any), 4 (CET-4), 6 (CET-6) |
| `max_frequency_rank` | int | — | Keep words with rank ≤ this value |
| `min_collins_stars` | int | — | 0-5 (minimum Collins rating) |
| `limit` | int | 20 | Max results (capped at `API_SEARCH_MAX_LIMIT`) |
| `offset` | int | 0 | Pagination offset |

### GET /api/v1/suggest

Autocomplete suggestions for search boxes. Uses the same filter semantics as `/search`.

| Query | Type | Default | Notes |
|-------|------|---------|-------|
| `prefix` | string | — | Search prefix (required; normalized minimum 3 Unicode characters after trimming, lowercasing, and removing spaces, hyphens, and underscores) |
| `cefr_level` | string | — | `A1`, `A2`, `B1`, `B2`, `C1`, or `C2` (case-insensitive) |
| `oxford_level` | int | — | 0 (any), 1 (Oxford 3000), 2 (Oxford 5000) |
| `cet_level` | int | — | 0 (any), 4 (CET-4), 6 (CET-6) |
| `max_frequency_rank` | int | — | Keep words with rank ≤ this value |
| `min_collins_stars` | int | — | 0-5 (minimum Collins rating) |
| `limit` | int | 10 | Max results (capped at `API_SUGGEST_MAX_LIMIT`) |

### GET /api/v1/phrases

Find phrases containing the specified keyword.

**Parameters:**
- `q` (required): Keyword to search for in phrases (1-50 characters)
- `limit`: Default 10, max 50

**Example:**
```bash
curl "http://localhost:8080/api/v1/phrases?q=run&limit=20"
```

## Operational Endpoints

### GET /health

Returns HTTP 200 with JSON health status. Intended for load balancers, uptime probes, and other lightweight operational monitoring.

**Response:**
```json
{
  "status": "ok",
  "service": "isdict-api"
}
```

**Note:** This endpoint does not use the standard response envelope for simplicity.

### GET /api/v1/health

Readiness endpoint. Verifies that the API can reach PostgreSQL by performing a database ping with the request context, and confirms that the required `pg_trgm` extension is already enabled.

**Success response (`200 OK`):**
```json
{
  "status": "ok",
  "service": "isdict-api"
}
```

**Failure response (`503 Service Unavailable`):**
```json
{
  "status": "not_ready",
  "service": "isdict-api"
}
```

`503` is returned when PostgreSQL is unreachable or when `pg_trgm` is missing.

**Note:** This endpoint also returns plain JSON and does not use the standard response envelope.

## Enumerations

### Accent Codes

`british`, `american`, `australian`, `newzealand`, `canadian`, `irish`, `scottish`, `indian`, `southafrican`, `other`, `unknown`

All accent parameters are case-insensitive.

### Part of Speech

`noun`, `verb`, `adjective`, `adverb`, `pronoun`, `preposition`, `conjunction`, `article`, `interjection`, `determiner`, `numeral`, `modal`, `auxiliary`, `particle`, `phrasal_verb`, `idiom`, `abbreviation`, `character`, `affix`, `contraction`, `punctuation`, `postposition`, `unknown`

All POS parameters are case-insensitive.

### Variant Form Types

`past`, `past_participle`, `present_3rd`, `gerund`, `infinitive`, `plural`, `possessive`, `comparative`, `superlative`

## Error Codes

| Code | Status | Description |
|------|--------|-------------|
| `WORD_NOT_FOUND` | 404 | Word or variant does not exist |
| `MISSING_PARAMETER` | 400 | Required path or query parameter missing |
| `INVALID_PARAMETER` | 400 | Validation failed for the supplied value |
| `BATCH_LIMIT_EXCEEDED` | 400 | Batch payload exceeds configured maximum |
| `INTERNAL_ERROR` | 500 | Unhandled server-side error; public message is always `An internal error occurred` |

Server-side internal error details are logged with request metadata and are not exposed in the API response body.

## Data Migration Authority

Data migration behavior is defined by `isdict-commons/migration` and should be treated as the authoritative implementation.

SQL files under `db/*.sql` in this repository are provided only as reference fixtures and for compatibility checks against disposable PostgreSQL databases; they are not the canonical migration source for production or compatibility guarantees.

## Usage Notes

- `/search` and `/suggest` enforce a minimum normalized query length of 3 Unicode characters after trimming, lowercasing, and removing spaces, hyphens, and underscores; `/phrases` accepts 1-50 trimmed characters
- Whitespace is automatically trimmed from all string parameters
- Enum parameters (accent, POS) are case-insensitive and normalized to lowercase
- Recommended: implement client-side caching for frequently accessed words and suggestions
- For production: apply gzip/brotli compression at reverse proxy layer for large responses
- Monitor connection pool settings (`DB_MAX_IDLE_CONNS`, `DB_MAX_OPEN_CONNS`) relative to PostgreSQL `max_connections`
- The batch endpoint (`/words/batch`) automatically deduplicates and removes empty entries
