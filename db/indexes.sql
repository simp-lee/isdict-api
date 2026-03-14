-- ==============================================================================
-- isdict Database Indexes
-- ==============================================================================
-- ⚠️  IMPORTANT: This SQL file is for REFERENCE and BACKWARD COMPATIBILITY
-- ==============================================================================
-- The authoritative index definitions are in Go code:
--   → isdict-commons/migration/migration.go (CreateIndexes method)
--
-- For production deployment, use the Go migration tool (recommended):
--   go run cmd/migrate-db/main.go --drop --force --confirm-drop postgres@db.example.com:5432/isdict
--
-- This SQL file may become outdated. Always verify against the Go code.
-- Last synchronized: 2026-03-08
-- ==============================================================================
--
-- Performance indexes for fast query execution
--
-- Prerequisites:
-- 1. Database schema created (run schema.sql first or use Go migration)
-- 2. pg_trgm extension enabled before creating trigram indexes
--
-- Legacy Usage (for backward compatibility):
-- psql --host "$PGHOST" --port "$PGPORT" --username "$PGUSER" --dbname "$PGDATABASE" -f db/indexes.sql
-- ==============================================================================

-- ==============================================================================
-- Unique constraints and business rules
-- ==============================================================================

-- Unique constraint: One primary pronunciation per word per accent
-- Example: "hello" can have only one primary British pronunciation
CREATE UNIQUE INDEX IF NOT EXISTS idx_pronunciation_primary_unique
ON pronunciations(word_id, accent) WHERE is_primary;

-- Unique constraint: Prevent duplicate word variants
-- Handles NULL form_type for alias variants (COALESCE converts NULL to 0)
CREATE UNIQUE INDEX IF NOT EXISTS idx_word_variant_unique
ON word_variants(word_id, variant_text, kind, COALESCE(form_type, 0));

-- ==============================================================================
-- Words table indexes
-- ==============================================================================

-- Normalized headword for case-insensitive search
CREATE INDEX IF NOT EXISTS idx_words_headword_normalized ON words(headword_normalized);

-- Filter indexes for learning levels
CREATE INDEX IF NOT EXISTS idx_words_cefr_level ON words(cefr_level);
CREATE INDEX IF NOT EXISTS idx_words_cet_level ON words(cet_level);
CREATE INDEX IF NOT EXISTS idx_words_oxford_level ON words(oxford_level);
CREATE INDEX IF NOT EXISTS idx_words_school_level ON words(school_level);

-- Frequency and popularity indexes
CREATE INDEX IF NOT EXISTS idx_words_frequency_rank ON words(frequency_rank);
CREATE INDEX IF NOT EXISTS idx_words_collins_stars ON words(collins_stars);

-- Foreign key indexes for repository lookups and cascade maintenance
CREATE INDEX IF NOT EXISTS idx_pronunciations_word_id ON pronunciations(word_id);
CREATE INDEX IF NOT EXISTS idx_senses_word_id ON senses(word_id);
CREATE INDEX IF NOT EXISTS idx_examples_sense_id ON examples(sense_id);

-- ==============================================================================
-- Word variants table indexes
-- ==============================================================================

-- Fast lookup by variant text
CREATE INDEX IF NOT EXISTS idx_word_variants_variant_text ON word_variants(variant_text);

-- Normalized variant for reverse lookup (finding main word from variant)
CREATE INDEX IF NOT EXISTS idx_word_variants_headword_normalized ON word_variants(headword_normalized);

-- Foreign key index for joins
CREATE INDEX IF NOT EXISTS idx_word_variants_word_id ON word_variants(word_id);

-- Frequency ranking for variants
CREATE INDEX IF NOT EXISTS idx_word_variants_frequency_rank ON word_variants(frequency_rank);

CREATE INDEX IF NOT EXISTS idx_words_headword_trgm ON words USING gin(headword_normalized gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_words_phrase_lower_trgm ON words USING gin((lower(headword)) gin_trgm_ops) WHERE headword LIKE '% %';
CREATE INDEX IF NOT EXISTS idx_word_variants_headword_trgm ON word_variants USING gin(headword_normalized gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_word_variants_phrase_lower_trgm ON word_variants USING gin((lower(variant_text)) gin_trgm_ops) WHERE variant_text LIKE '% %';

-- 
-- Index Summary:
--   • Unique indexes: 2 (data integrity)
--   • Non-unique B-tree indexes: 14 (exact/prefix/range/filter/join queries)
--   • GIN trigram indexes: 4 (fuzzy search optimization)
--   • Total: 20 indexes when pg_trgm is enabled
--
-- Verify indexes:
--   \di+ in psql to list all indexes with sizes
--   SELECT * FROM pg_indexes WHERE schemaname = 'public';
--
-- Performance tips:
--   • Trigram indexes require the pg_trgm extension
--   • Run ANALYZE after bulk data import
--   • Monitor index usage: SELECT * FROM pg_stat_user_indexes;
-- ==============================================================================

-- Update table statistics for query optimization
ANALYZE words;
ANALYZE word_variants;
ANALYZE pronunciations;
ANALYZE senses;
ANALYZE examples;
