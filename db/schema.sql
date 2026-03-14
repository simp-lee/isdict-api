-- ==============================================================================
-- isdict Database Schema
-- ==============================================================================
-- ⚠️  IMPORTANT: This SQL file is for REFERENCE and BACKWARD COMPATIBILITY
-- ==============================================================================
-- The authoritative database schema is defined in Go code:
--   → isdict-commons/migration/migration.go
--
-- For production deployment, use the Go migration tool (recommended):
--   go run cmd/migrate-db/main.go --drop --force --confirm-drop postgres@db.example.com:5432/isdict
--
-- This SQL file may become outdated. Always verify against the Go code.
-- Last synchronized: 2026-03-08
-- ==============================================================================
--
-- This schema defines the complete database structure for the isdict project.
-- Tables: words, pronunciations, senses, examples, word_variants
--
-- Prerequisites:
-- 1. PostgreSQL 14 or higher
-- 2. Provision an empty disposable database on your PostgreSQL instance
--
-- Legacy Usage (for backward compatibility):
-- psql --host "$PGHOST" --port "$PGPORT" --username "$PGUSER" --dbname "$PGDATABASE" -f db/schema.sql
-- ==============================================================================

-- Enable required performance extension.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ==============================================================================
-- Table: words (Main word entries)
-- ==============================================================================
CREATE TABLE IF NOT EXISTS words (
    id                  SERIAL PRIMARY KEY,
    headword            VARCHAR(255) NOT NULL UNIQUE,
    headword_normalized VARCHAR(255) NOT NULL,
    cefr_level          SMALLINT NOT NULL DEFAULT 0 CHECK (cefr_level BETWEEN 0 AND 6),
    cefr_source         VARCHAR(10) NOT NULL DEFAULT '' CHECK (cefr_source IN ('', 'oxford', 'cefrj', 'both')),
    cet_level           SMALLINT NOT NULL DEFAULT 0 CHECK (cet_level BETWEEN 0 AND 2),
    oxford_level        SMALLINT NOT NULL DEFAULT 0 CHECK (oxford_level BETWEEN 0 AND 2),
    school_level        SMALLINT NOT NULL DEFAULT 0 CHECK (school_level BETWEEN 0 AND 3),
    frequency_count     INTEGER NOT NULL DEFAULT 0 CHECK (frequency_count >= 0),
    frequency_rank      INTEGER NOT NULL DEFAULT 0 CHECK (frequency_rank >= 0),
    collins_stars       SMALLINT NOT NULL DEFAULT 0 CHECK (collins_stars BETWEEN 0 AND 5),
    translation_zh      TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE words IS 'Main word entries with learning annotations';
COMMENT ON COLUMN words.headword IS 'Original word form (case-sensitive)';
COMMENT ON COLUMN words.headword_normalized IS 'Normalized lowercase form for searching';
COMMENT ON COLUMN words.cefr_level IS 'CEFR level: 0=Unknown, 1=A1, 2=A2, 3=B1, 4=B2, 5=C1, 6=C2';
COMMENT ON COLUMN words.cefr_source IS 'Source of CEFR annotation: cefrj, oxford, both';
COMMENT ON COLUMN words.cet_level IS 'CET level: 0=Unknown, 1=CET-4, 2=CET-6';
COMMENT ON COLUMN words.oxford_level IS 'Oxford: 0=None, 1=Oxford 3000, 2=Oxford 5000';
COMMENT ON COLUMN words.frequency_rank IS 'Word frequency rank (lower = more common, 0=unknown)';

-- ==============================================================================
-- Table: pronunciations (Pronunciation variants with IPA)
-- ==============================================================================
CREATE TABLE IF NOT EXISTS pronunciations (
    id         SERIAL PRIMARY KEY,
    word_id    INTEGER NOT NULL REFERENCES words(id) ON DELETE CASCADE,
    accent     SMALLINT NOT NULL CHECK (accent BETWEEN 0 AND 10),
    ipa        VARCHAR(200) NOT NULL,
    is_primary BOOLEAN NOT NULL DEFAULT false,
    UNIQUE (word_id, accent, ipa)
);

COMMENT ON TABLE pronunciations IS 'IPA pronunciations for different accents';
COMMENT ON COLUMN pronunciations.accent IS 'Accent code: 0=Unknown, 1=British, 2=American, 3=Australian, etc.';
COMMENT ON COLUMN pronunciations.is_primary IS 'Whether this is the primary pronunciation for this accent';

-- ==============================================================================
-- Table: senses (Word senses/definitions)
-- ==============================================================================
CREATE TABLE IF NOT EXISTS senses (
    id            SERIAL PRIMARY KEY,
    word_id       INTEGER NOT NULL REFERENCES words(id) ON DELETE CASCADE,
    pos           SMALLINT NOT NULL CHECK (pos BETWEEN 0 AND 22),
    definition_en TEXT NOT NULL,
    definition_zh TEXT NOT NULL,
    sense_order   SMALLINT NOT NULL DEFAULT 1 CHECK (sense_order >= 1),
    cefr_level    SMALLINT NOT NULL DEFAULT 0 CHECK (cefr_level BETWEEN 0 AND 6),
    cefr_source   VARCHAR(10) NOT NULL DEFAULT '' CHECK (cefr_source IN ('', 'oxford', 'cefrj', 'both')),
    oxford_level  SMALLINT NOT NULL DEFAULT 0 CHECK (oxford_level BETWEEN 0 AND 2),
    UNIQUE (word_id, pos, sense_order)
);

COMMENT ON TABLE senses IS 'Word senses (definitions) with part-of-speech';
COMMENT ON COLUMN senses.pos IS 'Part of speech: 0=Unknown, 1=Noun, 2=Verb, 3=Adj, 4=Adv, etc.';
COMMENT ON COLUMN senses.sense_order IS 'Order of sense within the same POS';

-- ==============================================================================
-- Table: examples (Example sentences)
-- ==============================================================================
CREATE TABLE IF NOT EXISTS examples (
    id            SERIAL PRIMARY KEY,
    sense_id      INTEGER NOT NULL REFERENCES senses(id) ON DELETE CASCADE,
    sentence_en   TEXT NOT NULL,
    sentence_zh   TEXT,
    example_order SMALLINT NOT NULL DEFAULT 1 CHECK (example_order >= 1),
    UNIQUE (sense_id, example_order)
);

COMMENT ON TABLE examples IS 'Example sentences for word senses';
COMMENT ON COLUMN examples.example_order IS 'Order of example within the same sense';

-- ==============================================================================
-- Table: word_variants (Morphological forms and spelling variants)
-- ==============================================================================
CREATE TABLE IF NOT EXISTS word_variants (
    id                  SERIAL PRIMARY KEY,
    word_id             INTEGER NOT NULL REFERENCES words(id) ON DELETE CASCADE,
    variant_text        VARCHAR(255) NOT NULL,
    headword_normalized VARCHAR(255) NOT NULL DEFAULT '',
    kind                SMALLINT NOT NULL CHECK (kind BETWEEN 1 AND 2),
    form_type           SMALLINT CHECK (form_type BETWEEN 1 AND 9),
    tags                TEXT[],
    frequency_count     INTEGER NOT NULL DEFAULT 0 CHECK (frequency_count >= 0),
    frequency_rank      INTEGER NOT NULL DEFAULT 0 CHECK (frequency_rank >= 0),
    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT word_variants_kind_form_type_check CHECK (
        (kind = 1 AND form_type IS NOT NULL) OR
        (kind = 2 AND form_type IS NULL)
    )
);

COMMENT ON TABLE word_variants IS 'Word variants (forms, aliases, spelling variations)';
COMMENT ON COLUMN word_variants.kind IS 'Variant kind: 1=Form, 2=Alias';
COMMENT ON COLUMN word_variants.form_type IS 'Form type: 1=past, 2=past_participle, 3=present_3rd, 4=gerund, 5=plural, etc.';
COMMENT ON COLUMN word_variants.tags IS 'Additional tags (e.g., regional, archaic)';

-- ==============================================================================
-- Schema creation complete
-- ==============================================================================
-- Next steps:
-- 1. Run db/indexes.sql to create performance indexes
-- 2. Run db/sample_data.sql only against an empty disposable database (or import a full data dump)
-- ==============================================================================
