package repository

import (
	"errors"
	"fmt"
	"strings"

	"github.com/simp-lee/isdict-commons/model"
	"github.com/simp-lee/isdict-commons/textutil"
	"gorm.io/gorm"
)

// Type aliases for convenience
type (
	Word          = model.Word
	Pronunciation = model.Pronunciation
	Sense         = model.Sense
	Example       = model.Example
	WordVariant   = model.WordVariant
)

// Repository provides database access methods
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new repository instance and returns it as WordRepository interface
func NewRepository(db *gorm.DB) WordRepository {
	return &Repository{db: db}
}

// applyPreloads applies preload configurations to a query based on flags
// This eliminates code duplication across multiple query methods
func (r *Repository) applyPreloads(query *gorm.DB, includeVariants, includePronunciations, includeSenses bool) *gorm.DB {
	if includePronunciations {
		query = query.Preload("Pronunciations")
	}
	if includeSenses {
		query = query.Preload("Senses.Examples", func(db *gorm.DB) *gorm.DB {
			return db.Order("example_order ASC")
		}).Preload("Senses", func(db *gorm.DB) *gorm.DB {
			return db.Order("sense_order ASC")
		})
	}
	if includeVariants {
		query = query.Preload("WordVariants")
	}
	return query
}

// GetWordByHeadword retrieves a word by its headword with automatic fallback to variants
// Step 1: Try exact case-sensitive match on headword field
// Step 2: If not found, try normalized match (case-insensitive) with preference for lowercase
// Step 3: If still not found, search word_variants table
// Step 4: Return the main word associated with the variant
// Returns: (*Word, *WordVariant, error)
//   - Word: the main word entry
//   - WordVariant: nil if directly matched, or the matched variant if found via variant lookup
func (r *Repository) GetWordByHeadword(headword string, includeVariants, includePronunciations, includeSenses bool) (*Word, *WordVariant, error) {
	normalizedHeadword := textutil.ToNormalized(headword)

	// ============ Step 1: Try exact case-sensitive match first ============
	// Use headword field directly (case-sensitive in PostgreSQL)
	// Example: 'Polish' (proper noun) vs 'polish' (verb) are treated as different words
	var word Word
	query := r.db.Where("headword = ?", headword)
	query = r.applyPreloads(query, includeVariants, includePronunciations, includeSenses)

	err := query.First(&word).Error
	if err == nil {
		return &word, nil, nil // Exact case-sensitive match found
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err // Database error
	}

	// ============ Step 2: Try normalized match (case-insensitive) ============
	// If exact match fails, search by normalized form to handle case variations
	// Prefer lowercase variants (common words) over capitalized ones (proper nouns)
	query = r.db.Where("headword_normalized = ?", normalizedHeadword)
	query = r.applyPreloads(query, includeVariants, includePronunciations, includeSenses)
	query = query.Order("CASE WHEN headword = LOWER(headword) THEN 0 ELSE 1 END, id ASC")

	err = query.First(&word).Error
	if err == nil {
		return &word, nil, nil // Main word found by normalized form
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err // Database error
	}

	// ============ Step 3: Search word_variants table ============
	// Main word not found, search in variants table using normalized form
	// Join with words table to sort by quality metrics (frequency, CEFR level)
	var variant WordVariant
	err = r.db.
		Select("word_variants.*").
		Joins("INNER JOIN words ON word_variants.word_id = words.id").
		Where("word_variants.headword_normalized = ?", normalizedHeadword).
		Order(`word_variants.kind ASC, 
		       CASE WHEN words.frequency_rank = 0 THEN 999999 ELSE words.frequency_rank END ASC, 
		       words.cefr_level DESC, 
		       word_variants.word_id ASC`).
		First(&variant).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrWordNotFound // Not found in both tables
		}
		return nil, nil, err // Database error
	}

	// ============ Step 4: Found variant, retrieve the main word ============
	query = r.db.Where("id = ?", variant.WordID)
	query = r.applyPreloads(query, includeVariants, includePronunciations, includeSenses)

	if err := query.First(&word).Error; err != nil {
		return nil, nil, err
	}

	return &word, &variant, nil // Main word found via variant
}

// GetWordsByVariant finds words by variant text using normalized form matching
// Normalization ignores spaces, hyphens, and case, but preserves apostrophes and slashes
func (r *Repository) GetWordsByVariant(variant string, kind *int) ([]Word, []WordVariant, error) {
	var variants []WordVariant

	// Use normalized form for matching
	variantNormalized := textutil.ToNormalized(variant)

	query := r.db.Where("headword_normalized = ?", variantNormalized)

	if kind != nil {
		query = query.Where("kind = ?", *kind)
	}

	query = query.Order("kind ASC, COALESCE(form_type, 0) ASC, variant_text ASC")

	if err := query.Find(&variants).Error; err != nil {
		return nil, nil, err
	}

	if len(variants) == 0 {
		return nil, nil, ErrVariantNotFound
	}

	// Get unique word IDs
	idSet := make(map[uint]struct{}, len(variants))
	wordIDs := make([]uint, 0, len(variants))
	for _, v := range variants {
		if _, exists := idSet[v.WordID]; exists {
			continue
		}
		idSet[v.WordID] = struct{}{}
		wordIDs = append(wordIDs, v.WordID)
	}

	// Fetch words with all relations
	var words []Word
	err := r.db.Where("id IN ?", wordIDs).
		Preload("Pronunciations").
		Preload("Senses.Examples", func(db *gorm.DB) *gorm.DB {
			return db.Order("example_order ASC")
		}).
		Preload("Senses", func(db *gorm.DB) *gorm.DB {
			return db.Order("sense_order ASC")
		}).
		Order("frequency_rank ASC, headword ASC").
		Find(&words).Error

	if err != nil {
		return nil, nil, err
	}

	return words, variants, nil
}

// GetWordsByHeadwords retrieves multiple words by their headwords (batch query)
// Uses headword_normalized for matching, only queries main words table
func (r *Repository) GetWordsByHeadwords(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]Word, error) {
	if len(headwords) == 0 {
		return []Word{}, nil
	}

	// Convert to normalized forms
	normalizedForms := make([]string, len(headwords))
	for i, hw := range headwords {
		normalizedForms[i] = textutil.ToNormalized(hw)
	}

	var words []Word
	query := r.db.Where("headword_normalized IN ?", normalizedForms)
	query = r.applyPreloads(query, includeVariants, includePronunciations, includeSenses)

	if err := query.Find(&words).Error; err != nil {
		return nil, err
	}

	return words, nil
}

// escapeLikePattern escapes SQL LIKE wildcard characters to prevent user input from being interpreted as wildcards
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\") // Must be first to avoid double-escaping
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

// SearchWords performs fuzzy search on words using headword_normalized
// Optimized with UNION strategy to avoid slow LEFT JOIN full table scans
func (r *Repository) SearchWords(keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]Word, int64, error) {
	normalizedKeyword := textutil.ToNormalized(keyword)
	escaped := escapeLikePattern(normalizedKeyword)
	prefix := escaped + "%"
	fuzzy := "%" + escaped + "%"

	// Build condition strings and args for prefix/fuzzy branches separately
	wordPrefixFilters := []string{"headword_normalized LIKE ?"}
	wordFuzzyFilters := []string{"headword_normalized LIKE ?"}
	variantPrefixFilters := []string{"v.headword_normalized LIKE ?"}
	variantFuzzyFilters := []string{"v.headword_normalized LIKE ?"}

	if cefrLevel != nil {
		wordPrefixFilters = append(wordPrefixFilters, "cefr_level = ?")
		wordFuzzyFilters = append(wordFuzzyFilters, "cefr_level = ?")
		variantPrefixFilters = append(variantPrefixFilters, "w.cefr_level = ?")
		variantFuzzyFilters = append(variantFuzzyFilters, "w.cefr_level = ?")
	}

	if oxfordLevel != nil {
		wordPrefixFilters = append(wordPrefixFilters, "oxford_level = ?")
		wordFuzzyFilters = append(wordFuzzyFilters, "oxford_level = ?")
		variantPrefixFilters = append(variantPrefixFilters, "w.oxford_level = ?")
		variantFuzzyFilters = append(variantFuzzyFilters, "w.oxford_level = ?")
	}

	if cetLevel != nil {
		wordPrefixFilters = append(wordPrefixFilters, "cet_level = ?")
		wordFuzzyFilters = append(wordFuzzyFilters, "cet_level = ?")
		variantPrefixFilters = append(variantPrefixFilters, "w.cet_level = ?")
		variantFuzzyFilters = append(variantFuzzyFilters, "w.cet_level = ?")
	}

	if maxFrequencyRank != nil {
		wordPrefixFilters = append(wordPrefixFilters, "frequency_rank > 0 AND frequency_rank <= ?")
		wordFuzzyFilters = append(wordFuzzyFilters, "frequency_rank > 0 AND frequency_rank <= ?")
		variantPrefixFilters = append(variantPrefixFilters, "w.frequency_rank > 0 AND w.frequency_rank <= ?")
		variantFuzzyFilters = append(variantFuzzyFilters, "w.frequency_rank > 0 AND w.frequency_rank <= ?")
	}

	if minCollinsStars != nil {
		wordPrefixFilters = append(wordPrefixFilters, "collins_stars >= ?")
		wordFuzzyFilters = append(wordFuzzyFilters, "collins_stars >= ?")
		variantPrefixFilters = append(variantPrefixFilters, "w.collins_stars >= ?")
		variantFuzzyFilters = append(variantFuzzyFilters, "w.collins_stars >= ?")
	}

	if pos != nil {
		wordPrefixFilters = append(wordPrefixFilters, "EXISTS (SELECT 1 FROM senses WHERE senses.word_id = id AND senses.pos = ?)")
		wordFuzzyFilters = append(wordFuzzyFilters, "EXISTS (SELECT 1 FROM senses WHERE senses.word_id = id AND senses.pos = ?)")
		variantPrefixFilters = append(variantPrefixFilters, "EXISTS (SELECT 1 FROM senses WHERE senses.word_id = w.id AND senses.pos = ?)")
		variantFuzzyFilters = append(variantFuzzyFilters, "EXISTS (SELECT 1 FROM senses WHERE senses.word_id = w.id AND senses.pos = ?)")
	}

	// Build COUNT query with all 4 UNION branches
	countSQL := fmt.Sprintf(`
		SELECT COUNT(DISTINCT id) FROM (
			SELECT id FROM words WHERE %s
			UNION
			SELECT id FROM words WHERE %s
			UNION
			SELECT word_id AS id FROM word_variants v
			INNER JOIN words w ON v.word_id = w.id
			WHERE %s
			UNION
			SELECT word_id AS id FROM word_variants v
			INNER JOIN words w ON v.word_id = w.id
			WHERE %s
		) AS matched
	`,
		strings.Join(wordPrefixFilters, " AND "),
		strings.Join(wordFuzzyFilters, " AND "),
		strings.Join(variantPrefixFilters, " AND "),
		strings.Join(variantFuzzyFilters, " AND "),
	)

	// Helper function to build args in correct order
	buildFilterArgs := func(pattern string) []interface{} {
		args := []interface{}{pattern}
		if cefrLevel != nil {
			args = append(args, *cefrLevel)
		}
		if oxfordLevel != nil {
			args = append(args, *oxfordLevel)
		}
		if cetLevel != nil {
			args = append(args, *cetLevel)
		}
		if maxFrequencyRank != nil {
			args = append(args, *maxFrequencyRank)
		}
		if minCollinsStars != nil {
			args = append(args, *minCollinsStars)
		}
		if pos != nil {
			args = append(args, *pos)
		}
		return args
	}

	// Build args for all 4 branches: prefix, fuzzy, prefix, fuzzy
	countArgs := []interface{}{}
	countArgs = append(countArgs, buildFilterArgs(prefix)...)
	countArgs = append(countArgs, buildFilterArgs(fuzzy)...)
	countArgs = append(countArgs, buildFilterArgs(prefix)...)
	countArgs = append(countArgs, buildFilterArgs(fuzzy)...)

	var total int64
	if err := r.db.Raw(countSQL, countArgs...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []Word{}, 0, nil
	}

	// Step 2: Get paginated results with priority ordering
	pageSQL := fmt.Sprintf(`
		WITH combined AS (
			SELECT 
				id,
				1 AS priority,
				CASE WHEN frequency_rank = 0 THEN 999999 ELSE frequency_rank END AS freq_rank,
				headword
			FROM words
			WHERE %s
			UNION ALL
			SELECT 
				id,
				3 AS priority,
				CASE WHEN frequency_rank = 0 THEN 999999 ELSE frequency_rank END AS freq_rank,
				headword
			FROM words
			WHERE %s
			UNION ALL
			SELECT 
				v.word_id AS id,
				2 AS priority,
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
				w.headword
			FROM word_variants v
			INNER JOIN words w ON v.word_id = w.id
			WHERE %s
			UNION ALL
			SELECT 
				v.word_id AS id,
				4 AS priority,
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
				w.headword
			FROM word_variants v
			INNER JOIN words w ON v.word_id = w.id
			WHERE %s
		), ranked AS (
			SELECT 
				id,
				priority,
				freq_rank,
				headword,
				ROW_NUMBER() OVER (
					PARTITION BY id
					ORDER BY freq_rank ASC, priority ASC, headword ASC
				) AS rn
			FROM combined
		)
		SELECT id, priority, freq_rank
		FROM ranked
		WHERE rn = 1
		ORDER BY freq_rank ASC, priority ASC, headword ASC
		LIMIT ? OFFSET ?
	`,
		strings.Join(wordPrefixFilters, " AND "),
		strings.Join(wordFuzzyFilters, " AND "),
		strings.Join(variantPrefixFilters, " AND "),
		strings.Join(variantFuzzyFilters, " AND "),
	)

	// Build args for all 4 branches: prefix, fuzzy, prefix, fuzzy
	pageArgs := []interface{}{}
	pageArgs = append(pageArgs, buildFilterArgs(prefix)...)
	pageArgs = append(pageArgs, buildFilterArgs(fuzzy)...)
	pageArgs = append(pageArgs, buildFilterArgs(prefix)...)
	pageArgs = append(pageArgs, buildFilterArgs(fuzzy)...)
	pageArgs = append(pageArgs, limit, offset)

	type idResult struct {
		ID       uint
		Priority int
		FreqRank int64
	}
	var results []idResult
	if err := r.db.Raw(pageSQL, pageArgs...).Scan(&results).Error; err != nil {
		return nil, 0, err
	}

	if len(results) == 0 {
		return []Word{}, total, nil
	}

	// Extract IDs (already deduplicated by SQL ROW_NUMBER)
	wordIDs := make([]uint, len(results))
	for i, r := range results {
		wordIDs[i] = r.ID
	}

	// Fetch full word objects
	var words []Word
	if err := r.db.Where("id IN ?", wordIDs).Find(&words).Error; err != nil {
		return nil, 0, err
	}

	// Load distinct POS values for each word
	type posRow struct {
		WordID uint
		POS    int
	}

	var posRows []posRow
	if err := r.db.Table("senses").
		Select("word_id, pos").
		Where("word_id IN ?", wordIDs).
		Group("word_id, pos").
		Find(&posRows).Error; err != nil {
		return nil, 0, err
	}

	posMap := make(map[uint][]Sense, len(wordIDs))
	for _, row := range posRows {
		posMap[row.WordID] = append(posMap[row.WordID], Sense{POS: row.POS})
	}

	// Build word map for reordering
	wordByID := make(map[uint]*Word, len(words))
	for i := range words {
		if senses, ok := posMap[words[i].ID]; ok {
			words[i].Senses = senses
		} else {
			words[i].Senses = []Sense{}
		}
		wordByID[words[i].ID] = &words[i]
	}

	// Re-order words to match the query result order
	orderedWords := make([]Word, 0, len(wordIDs))
	for _, id := range wordIDs {
		if w, ok := wordByID[id]; ok {
			orderedWords = append(orderedWords, *w)
		}
	}

	return orderedWords, total, nil
}

// SuggestWords provides autocomplete suggestions using headword_normalized
func (r *Repository) SuggestWords(prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]Word, error) {
	normalizedPrefix := textutil.ToNormalized(prefix)
	escaped := escapeLikePattern(normalizedPrefix) + "%"

	// Optimization: Use UNION ALL + ROW_NUMBER for efficient deduplication
	// This is faster than UNION which compares all columns

	// Build the UNION ALL query with window function for deduplication
	// Note: Use CASE to treat frequency_rank=0 as lowest priority (999999)
	unionSQL := `
		WITH combined AS (
			SELECT 
				id, 
				CASE WHEN frequency_rank = 0 THEN 999999 ELSE frequency_rank END AS frequency_rank
			FROM words 
			WHERE headword_normalized LIKE ?`

	args := []interface{}{escaped}

	if cefrLevel != nil {
		unionSQL += " AND cefr_level = ?"
		args = append(args, *cefrLevel)
	}
	if oxfordLevel != nil {
		unionSQL += " AND oxford_level = ?"
		args = append(args, *oxfordLevel)
	}
	if cetLevel != nil {
		unionSQL += " AND cet_level = ?"
		args = append(args, *cetLevel)
	}
	if maxFrequencyRank != nil {
		unionSQL += " AND frequency_rank > 0 AND frequency_rank <= ?"
		args = append(args, *maxFrequencyRank)
	}
	if minCollinsStars != nil {
		unionSQL += " AND collins_stars >= ?"
		args = append(args, *minCollinsStars)
	}

	unionSQL += `
			UNION ALL
			SELECT 
				word_id AS id, 
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS frequency_rank
			FROM word_variants v
			INNER JOIN words w ON v.word_id = w.id
			WHERE v.headword_normalized LIKE ?`

	args = append(args, escaped)

	if cefrLevel != nil {
		unionSQL += " AND w.cefr_level = ?"
		args = append(args, *cefrLevel)
	}
	if oxfordLevel != nil {
		unionSQL += " AND w.oxford_level = ?"
		args = append(args, *oxfordLevel)
	}
	if cetLevel != nil {
		unionSQL += " AND w.cet_level = ?"
		args = append(args, *cetLevel)
	}
	if maxFrequencyRank != nil {
		unionSQL += " AND w.frequency_rank > 0 AND w.frequency_rank <= ?"
		args = append(args, *maxFrequencyRank)
	}
	if minCollinsStars != nil {
		unionSQL += " AND w.collins_stars >= ?"
		args = append(args, *minCollinsStars)
	}

	unionSQL += `
		), ranked AS (
			SELECT 
				id,
				frequency_rank,
				ROW_NUMBER() OVER (
					PARTITION BY id
					ORDER BY frequency_rank ASC, id ASC
				) AS rn
			FROM combined
		)
		SELECT id, frequency_rank
		FROM ranked
		WHERE rn = 1
		ORDER BY frequency_rank ASC, id ASC
		LIMIT ?
	`

	args = append(args, limit)

	// Use a struct to scan both id and frequency_rank
	type idResult struct {
		ID uint
	}
	var results []idResult
	if err := r.db.Raw(unionSQL, args...).Scan(&results).Error; err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return []Word{}, nil
	}

	// Extract IDs (already deduplicated by SQL ROW_NUMBER)
	wordIDs := make([]uint, len(results))
	for i, r := range results {
		wordIDs[i] = r.ID
	}

	// Fetch full word objects, preserving the order from the query
	var words []Word
	if err := r.db.Where("id IN ?", wordIDs).Find(&words).Error; err != nil {
		return nil, err
	}

	// Re-order words to match the order from the UNION query
	// This is necessary because IN clause doesn't preserve order
	wordByID := make(map[uint]*Word, len(words))
	for i := range words {
		wordByID[words[i].ID] = &words[i]
	}

	orderedWords := make([]Word, 0, len(wordIDs))
	for _, id := range wordIDs {
		if w, ok := wordByID[id]; ok {
			orderedWords = append(orderedWords, *w)
		}
	}

	return orderedWords, nil
}

// SearchPhrases searches for phrases containing the keyword as a complete word
// A phrase is defined as a headword containing spaces
// The keyword must match as a complete word (word boundary matching)
func (r *Repository) SearchPhrases(keyword string, limit int) ([]Word, error) {
	if limit <= 0 {
		limit = 20
	}

	// Use lowercase for case-insensitive matching, but PRESERVE spaces
	// NOTE: We cannot use headword_normalized here because ToNormalized() removes spaces,
	// which would break phrase matching (e.g., "go after" becomes "goafter")
	lowerKeyword := strings.ToLower(strings.TrimSpace(keyword))
	escaped := escapeLikePattern(lowerKeyword)

	// Check if keyword contains spaces (indicates multi-word input for prefix matching)
	hasSpace := strings.Contains(lowerKeyword, " ")

	// Query for phrases (headwords containing spaces) where the keyword appears as a complete word
	// We search in LOWER(headword) for case-insensitive matching while preserving spaces
	// Pattern: keyword at start, middle, or end of phrase
	// - Start: "go %" matches "go after", "go Dutch"
	// - Middle: "% go %" matches "let go of"
	// - End: "% go" matches "let go"
	//
	// For incomplete input like "how a", we support prefix matching on the last word:
	// - "how a%" matches "how about", "how are you"
	//
	// IMPORTANT: For single-word searches (no spaces), we use word-boundary matching only.
	// This prevents "star" from matching "start in" or "start up".

	// Prepare LIKE patterns
	patternStart := escaped + " %"
	patternMiddle := "% " + escaped + " %"
	patternEnd := "% " + escaped
	// Add prefix pattern for autocomplete-style search (e.g., "how a" -> "how a%")
	// Only use unbounded prefix for multi-word queries; single words must have word boundaries
	var patternPrefix string
	if hasSpace {
		// Multi-word input: allow prefix matching on the last word
		// e.g., "how a" matches "how about", "how are you"
		patternPrefix = escaped + "%"
	} else {
		// Single-word input: require space boundary to avoid "star" matching "start"
		// This makes patternPrefix the same as patternStart, which is intentional
		patternPrefix = escaped + " %"
	}

	// Build CTE with UNION ALL so each branch can leverage trigram indexes individually
	// Priority:
	//  1. Keyword at start of main headword (exact + prefix)
	//  2. Keyword at start of variant (exact + prefix)
	//  3. Keyword in middle of main headword
	//  4. Keyword in middle of variant
	//  5. Keyword at end of main headword
	//  6. Keyword at end of variant
	//  7/8. Fallback tiers (should rarely trigger, kept for deterministic ordering)
	unionSQL := `
	WITH combined AS (
		SELECT
			w.id,
			w.id AS id_ord,
			CASE
				WHEN LOWER(w.headword) LIKE ? THEN 1
				WHEN LOWER(w.headword) LIKE ? THEN 1
				WHEN LOWER(w.headword) LIKE ? THEN 3
				WHEN LOWER(w.headword) LIKE ? THEN 5
				ELSE 7
			END AS priority,
			CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
			w.headword
		FROM words w
		WHERE w.headword LIKE '% %'
			AND (
				LOWER(w.headword) LIKE ?
				OR LOWER(w.headword) LIKE ?
				OR LOWER(w.headword) LIKE ?
				OR LOWER(w.headword) LIKE ?
			)

		UNION ALL

		SELECT
			v.word_id AS id,
			w.id AS id_ord,
			CASE
				WHEN LOWER(v.variant_text) LIKE ? THEN 2
				WHEN LOWER(v.variant_text) LIKE ? THEN 2
				WHEN LOWER(v.variant_text) LIKE ? THEN 4
				WHEN LOWER(v.variant_text) LIKE ? THEN 6
				ELSE 8
			END AS priority,
			CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
			w.headword
		FROM word_variants v
		JOIN words w ON w.id = v.word_id
		WHERE w.headword LIKE '% %'
			AND v.variant_text LIKE '% %'
			AND (
				LOWER(v.variant_text) LIKE ?
				OR LOWER(v.variant_text) LIKE ?
				OR LOWER(v.variant_text) LIKE ?
				OR LOWER(v.variant_text) LIKE ?
			)
	), ranked AS (
		SELECT
			id,
			priority,
			freq_rank,
			id_ord,
			headword,
			ROW_NUMBER() OVER (
				PARTITION BY id
				ORDER BY freq_rank, priority, id_ord, headword
			) AS rn
		FROM combined
	)
	SELECT id, priority, freq_rank, id_ord, headword
	FROM ranked
	WHERE rn = 1
	ORDER BY freq_rank, priority, id_ord, headword
	LIMIT ?
	`

	args := []interface{}{
		patternStart, patternPrefix, patternMiddle, patternEnd,
		patternStart, patternPrefix, patternMiddle, patternEnd,
		patternStart, patternPrefix, patternMiddle, patternEnd,
		patternStart, patternPrefix, patternMiddle, patternEnd,
		limit,
	}

	type phraseRow struct {
		ID       uint
		Priority int
		FreqRank int
		IDOrd    uint
		Headword string
	}

	var rankedRows []phraseRow
	if err := r.db.Raw(unionSQL, args...).Scan(&rankedRows).Error; err != nil {
		return nil, err
	}

	if len(rankedRows) == 0 {
		return []Word{}, nil
	}

	wordIDs := make([]uint, 0, len(rankedRows))
	for _, row := range rankedRows {
		wordIDs = append(wordIDs, row.ID)
	}
	if len(wordIDs) == 0 {
		return []Word{}, nil
	}

	// Fetch full word objects with POS info
	var words []Word
	if err := r.db.Where("id IN ?", wordIDs).
		Order("frequency_rank ASC, headword ASC").Find(&words).Error; err != nil {
		return nil, err
	}

	// Load distinct POS values for each word
	type posRow struct {
		WordID uint
		POS    int
	}

	var posRows []posRow
	if err := r.db.Table("senses").
		Select("word_id, pos").
		Where("word_id IN ?", wordIDs).
		Group("word_id, pos").
		Find(&posRows).Error; err != nil {
		return nil, err
	}

	posMap := make(map[uint][]Sense, len(wordIDs))
	for _, row := range posRows {
		posMap[row.WordID] = append(posMap[row.WordID], Sense{POS: row.POS})
	}

	for i := range words {
		if senses, ok := posMap[words[i].ID]; ok {
			words[i].Senses = senses
		} else {
			words[i].Senses = []Sense{}
		}
	}

	wordByID := make(map[uint]*Word, len(words))
	for i := range words {
		wordByID[words[i].ID] = &words[i]
	}

	ordered := make([]Word, 0, len(wordIDs))
	for _, id := range wordIDs {
		if w, ok := wordByID[id]; ok {
			ordered = append(ordered, *w)
		}
	}

	return ordered, nil
}

// GetPronunciationsByWordID retrieves pronunciations for a word
func (r *Repository) GetPronunciationsByWordID(wordID uint, accent *int) ([]Pronunciation, error) {
	var pronunciations []Pronunciation
	query := r.db.Where("word_id = ?", wordID)

	if accent != nil {
		query = query.Where("accent = ?", *accent)
	}

	if err := query.Find(&pronunciations).Error; err != nil {
		return nil, err
	}

	return pronunciations, nil
}

// GetSensesByWordID retrieves senses for a word
func (r *Repository) GetSensesByWordID(wordID uint, pos *int) ([]Sense, error) {
	var senses []Sense
	query := r.db.Where("word_id = ?", wordID)

	if pos != nil {
		query = query.Where("pos = ?", *pos)
	}

	if err := query.Preload("Examples", func(db *gorm.DB) *gorm.DB {
		return db.Order("example_order ASC")
	}).Order("sense_order ASC").Find(&senses).Error; err != nil {
		return nil, err
	}

	return senses, nil
}
