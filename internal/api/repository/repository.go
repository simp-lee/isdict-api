package repository

import (
	"context"
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

type searchFilters struct {
	pos              *int
	cefrLevel        *int
	oxfordLevel      *int
	cetLevel         *int
	maxFrequencyRank *int
	minCollinsStars  *int
}

type searchResultID struct {
	ID       uint
	Priority int
	FreqRank int64
}

type suggestionResultID struct {
	ID uint
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
func (r *Repository) GetWordByHeadword(ctx context.Context, headword string, includeVariants, includePronunciations, includeSenses bool) (*Word, *WordVariant, error) {
	normalizedHeadword := textutil.ToNormalized(headword)
	db := r.db.WithContext(ctx)

	// ============ Step 1: Try exact case-sensitive match first ============
	// Use headword field directly (case-sensitive in PostgreSQL)
	// Example: 'Polish' (proper noun) vs 'polish' (verb) are treated as different words
	var word Word
	query := db.Where("headword = ?", headword)
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
	query = db.Where("headword_normalized = ?", normalizedHeadword)
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
	variantRanking := `word_variants.kind ASC,
	       CASE WHEN words.frequency_rank = 0 THEN 999999 ELSE words.frequency_rank END ASC,
	       words.cefr_level DESC,
	       word_variants.word_id ASC`
	err = db.
		Select("word_variants.*").
		Joins("INNER JOIN words ON word_variants.word_id = words.id").
		Where("word_variants.variant_text = ?", headword).
		Order(variantRanking).
		First(&variant).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err // Database error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = db.
			Select("word_variants.*").
			Joins("INNER JOIN words ON word_variants.word_id = words.id").
			Where("word_variants.headword_normalized = ?", normalizedHeadword).
			Order(variantRanking).
			First(&variant).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrWordNotFound // Not found in both tables
		}
		return nil, nil, err // Database error
	}

	// ============ Step 4: Found variant, retrieve the main word ============
	query = db.Where("id = ?", variant.WordID)
	query = r.applyPreloads(query, includeVariants, includePronunciations, includeSenses)

	if err := query.First(&word).Error; err != nil {
		return nil, nil, err
	}

	return &word, &variant, nil // Main word found via variant
}

// GetWordsByVariant finds words by variant text using normalized form matching.
// Normalization ignores spaces, hyphens, and case, but preserves apostrophes and slashes.
func (r *Repository) GetWordsByVariant(ctx context.Context, variant string, kind *int, includePronunciations, includeSenses bool) ([]Word, []WordVariant, error) {
	var variants []WordVariant
	db := r.db.WithContext(ctx)

	// Use normalized form for matching
	variantNormalized := textutil.ToNormalized(variant)

	query := db.Where("headword_normalized = ?", variantNormalized)

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

	// Fetch words with only the relations the caller asked for.
	var words []Word
	query = db.Where("id IN ?", wordIDs)
	query = r.applyPreloads(query, false, includePronunciations, includeSenses)
	err := query.Order("CASE WHEN frequency_rank = 0 THEN 999999 ELSE frequency_rank END ASC, headword ASC").Find(&words).Error

	if err != nil {
		return nil, nil, err
	}

	return words, variants, nil
}

// GetWordsByVariants resolves multiple variant headwords in one query.
// It de-duplicates normalized forms for the SQL lookup, then maps results back to
// the original request order so each successful input gets its own word/variant match.
func (r *Repository) GetWordsByVariants(ctx context.Context, variants []string, includeVariants, includePronunciations, includeSenses bool) ([]BatchVariantMatch, error) {
	if len(variants) == 0 {
		return []BatchVariantMatch{}, nil
	}

	db := r.db.WithContext(ctx)
	normalizedInputs, normalizedForms := normalizeUniqueInputs(variants)
	if len(normalizedForms) == 0 {
		return []BatchVariantMatch{}, nil
	}

	var rankedVariants []WordVariant
	err := db.
		Select("word_variants.*").
		Joins("INNER JOIN words ON word_variants.word_id = words.id").
		Where("word_variants.headword_normalized IN ?", normalizedForms).
		Order(`word_variants.headword_normalized ASC,
		       word_variants.kind ASC,
		       CASE WHEN words.frequency_rank = 0 THEN 999999 ELSE words.frequency_rank END ASC,
		       words.cefr_level DESC,
		       word_variants.word_id ASC`).
		Find(&rankedVariants).Error
	if err != nil {
		return nil, err
	}

	if len(rankedVariants) == 0 {
		return []BatchVariantMatch{}, nil
	}

	variantsByNormalized, wordIDs := groupVariantsByNormalized(rankedVariants, len(normalizedForms))

	var words []Word
	query := db.Where("id IN ?", wordIDs)
	query = r.applyPreloads(query, includeVariants, includePronunciations, includeSenses)
	if err := query.Find(&words).Error; err != nil {
		return nil, err
	}

	return buildBatchVariantMatches(variants, normalizedInputs, variantsByNormalized, indexWordsByID(words)), nil
}

// GetWordsByHeadwords retrieves multiple words by their headwords (batch query)
// Uses headword_normalized for matching, only queries main words table
func (r *Repository) GetWordsByHeadwords(ctx context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]Word, error) {
	if len(headwords) == 0 {
		return []Word{}, nil
	}
	db := r.db.WithContext(ctx)

	// Convert to normalized forms
	normalizedForms := make([]string, len(headwords))
	for i, hw := range headwords {
		normalizedForms[i] = textutil.ToNormalized(hw)
	}

	var words []Word
	query := db.Where("headword_normalized IN ?", normalizedForms)
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

func newSearchFilters(pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int) searchFilters {
	return searchFilters{
		pos:              pos,
		cefrLevel:        cefrLevel,
		oxfordLevel:      oxfordLevel,
		cetLevel:         cetLevel,
		maxFrequencyRank: maxFrequencyRank,
		minCollinsStars:  minCollinsStars,
	}
}

func (filters searchFilters) clauses(textAlias, wordAlias string) []string {
	clauses := []string{fmt.Sprintf("%s.headword_normalized LIKE ?", textAlias)}
	if filters.cefrLevel != nil {
		clauses = append(clauses, fmt.Sprintf("%s.cefr_level = ?", wordAlias))
	}
	if filters.oxfordLevel != nil {
		clauses = append(clauses, fmt.Sprintf("%s.oxford_level = ?", wordAlias))
	}
	if filters.cetLevel != nil {
		clauses = append(clauses, fmt.Sprintf("%s.cet_level = ?", wordAlias))
	}
	if filters.maxFrequencyRank != nil {
		clauses = append(clauses, fmt.Sprintf("%s.frequency_rank > 0 AND %s.frequency_rank <= ?", wordAlias, wordAlias))
	}
	if filters.minCollinsStars != nil {
		clauses = append(clauses, fmt.Sprintf("%s.collins_stars >= ?", wordAlias))
	}
	if filters.pos != nil {
		clauses = append(clauses, fmt.Sprintf("EXISTS (SELECT 1 FROM senses s WHERE s.word_id = %s.id AND s.pos = ?)", wordAlias))
	}
	return clauses
}

func (filters searchFilters) args(pattern string) []interface{} {
	args := []interface{}{pattern}
	if filters.cefrLevel != nil {
		args = append(args, *filters.cefrLevel)
	}
	if filters.oxfordLevel != nil {
		args = append(args, *filters.oxfordLevel)
	}
	if filters.cetLevel != nil {
		args = append(args, *filters.cetLevel)
	}
	if filters.maxFrequencyRank != nil {
		args = append(args, *filters.maxFrequencyRank)
	}
	if filters.minCollinsStars != nil {
		args = append(args, *filters.minCollinsStars)
	}
	if filters.pos != nil {
		args = append(args, *filters.pos)
	}
	return args
}

func appendArgSets(argSets ...[]interface{}) []interface{} {
	combined := make([]interface{}, 0)
	for _, argSet := range argSets {
		combined = append(combined, argSet...)
	}
	return combined
}

func normalizeUniqueInputs(inputs []string) ([]string, []string) {
	normalizedInputs := make([]string, len(inputs))
	normalizedForms := make([]string, 0, len(inputs))
	seenNormalized := make(map[string]struct{}, len(inputs))
	for i, input := range inputs {
		normalized := textutil.ToNormalized(input)
		normalizedInputs[i] = normalized
		if normalized == "" {
			continue
		}
		if _, exists := seenNormalized[normalized]; exists {
			continue
		}
		seenNormalized[normalized] = struct{}{}
		normalizedForms = append(normalizedForms, normalized)
	}
	return normalizedInputs, normalizedForms
}

func groupVariantsByNormalized(rankedVariants []WordVariant, capacity int) (map[string][]WordVariant, []uint) {
	variantsByNormalized := make(map[string][]WordVariant, capacity)
	selectedWordIDs := make(map[uint]struct{}, len(rankedVariants))
	wordIDs := make([]uint, 0, len(rankedVariants))
	for _, variant := range rankedVariants {
		variantsByNormalized[variant.HeadwordNormalized] = append(variantsByNormalized[variant.HeadwordNormalized], variant)
		if _, exists := selectedWordIDs[variant.WordID]; exists {
			continue
		}
		selectedWordIDs[variant.WordID] = struct{}{}
		wordIDs = append(wordIDs, variant.WordID)
	}
	return variantsByNormalized, wordIDs
}

func indexWordsByID(words []Word) map[uint]Word {
	indexed := make(map[uint]Word, len(words))
	for _, word := range words {
		indexed[word.ID] = word
	}
	return indexed
}

func selectVariantCandidate(candidates []WordVariant, input string) (WordVariant, bool) {
	if len(candidates) == 0 {
		return WordVariant{}, false
	}
	for _, candidate := range candidates {
		if candidate.VariantText == input {
			return candidate, true
		}
	}
	return candidates[0], true
}

func buildBatchVariantMatches(inputs []string, normalizedInputs []string, variantsByNormalized map[string][]WordVariant, wordsByID map[uint]Word) []BatchVariantMatch {
	matches := make([]BatchVariantMatch, 0, len(inputs))
	for i, input := range inputs {
		normalized := normalizedInputs[i]
		if normalized == "" {
			continue
		}
		selected, ok := selectVariantCandidate(variantsByNormalized[normalized], input)
		if !ok {
			continue
		}
		word, exists := wordsByID[selected.WordID]
		if !exists {
			continue
		}
		matches = append(matches, BatchVariantMatch{Word: word, Variant: selected})
	}
	return matches
}

// SearchWords performs fuzzy search on words using headword_normalized
// Optimized with UNION strategy to avoid slow LEFT JOIN full table scans
func (r *Repository) SearchWords(ctx context.Context, keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]Word, int64, error) {
	normalizedKeyword := textutil.ToNormalized(keyword)
	escaped := escapeLikePattern(normalizedKeyword)
	prefix := escaped + "%"
	fuzzy := "%" + escaped + "%"
	db := r.db.WithContext(ctx)
	filters := newSearchFilters(pos, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars)
	wordPrefixFilters := filters.clauses("w", "w")
	wordFuzzyFilters := filters.clauses("w", "w")
	variantPrefixFilters := filters.clauses("v", "w")
	variantFuzzyFilters := filters.clauses("v", "w")

	// Build COUNT query with all 4 UNION branches
	countSQL := fmt.Sprintf(`
		SELECT COUNT(DISTINCT id) FROM (
			SELECT w.id FROM words w WHERE %s
			UNION
			SELECT w.id FROM words w WHERE %s
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

	prefixArgs := filters.args(prefix)
	fuzzyArgs := filters.args(fuzzy)
	countArgs := appendArgSets(prefixArgs, fuzzyArgs, prefixArgs, fuzzyArgs)

	var total int64
	if err := db.Raw(countSQL, countArgs...).Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []Word{}, 0, nil
	}

	// Step 2: Get paginated results with priority ordering
	pageSQL := fmt.Sprintf(`
		WITH combined AS (
			SELECT 
				w.id,
				1 AS priority,
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
				w.headword
			FROM words w
			WHERE %s
			UNION ALL
			SELECT 
				w.id,
				3 AS priority,
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
				w.headword
			FROM words w
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

	pageArgs := append(appendArgSets(prefixArgs, fuzzyArgs, prefixArgs, fuzzyArgs), limit, offset)

	var results []searchResultID
	if err := db.Raw(pageSQL, pageArgs...).Scan(&results).Error; err != nil {
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
	if err := db.Where("id IN ?", wordIDs).Find(&words).Error; err != nil {
		return nil, 0, err
	}

	// Load distinct POS values for each word
	type posRow struct {
		WordID uint
		POS    int
	}

	var posRows []posRow
	posQuery := db.Table("senses").
		Select("word_id, pos").
		Where("word_id IN ?", wordIDs)
	if pos != nil {
		posQuery = posQuery.Where("pos = ?", *pos)
	}
	if err := posQuery.
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
func (r *Repository) SuggestWords(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]Word, error) {
	normalizedPrefix := textutil.ToNormalized(prefix)
	escaped := escapeLikePattern(normalizedPrefix) + "%"
	db := r.db.WithContext(ctx)
	filters := newSearchFilters(nil, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars)
	wordFilters := strings.Join(filters.clauses("w", "w"), " AND ")
	variantFilters := strings.Join(filters.clauses("v", "w"), " AND ")

	// Optimization: Use UNION ALL + ROW_NUMBER for efficient deduplication
	// This is faster than UNION which compares all columns

	// Build the UNION ALL query with window function for deduplication
	// Note: Use CASE to treat frequency_rank=0 as lowest priority (999999)
	unionSQL := fmt.Sprintf(`
		WITH combined AS (
			SELECT 
				id, 
				CASE WHEN frequency_rank = 0 THEN 999999 ELSE frequency_rank END AS frequency_rank
			FROM words w
			WHERE %s
			UNION ALL
			SELECT 
				word_id AS id, 
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS frequency_rank
			FROM word_variants v
			INNER JOIN words w ON v.word_id = w.id
			WHERE %s
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
	`, wordFilters, variantFilters)

	args := append(appendArgSets(filters.args(escaped), filters.args(escaped)), limit)

	var results []suggestionResultID
	if err := db.Raw(unionSQL, args...).Scan(&results).Error; err != nil {
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
	if err := db.Where("id IN ?", wordIDs).Find(&words).Error; err != nil {
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
func (r *Repository) SearchPhrases(ctx context.Context, keyword string, limit int) ([]Word, error) {
	if limit <= 0 {
		limit = 20
	}
	db := r.db.WithContext(ctx)

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
	if err := db.Raw(unionSQL, args...).Scan(&rankedRows).Error; err != nil {
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
	if err := db.Where("id IN ?", wordIDs).
		Order("frequency_rank ASC, headword ASC").Find(&words).Error; err != nil {
		return nil, err
	}

	// Load distinct POS values for each word
	type posRow struct {
		WordID uint
		POS    int
	}

	var posRows []posRow
	if err := db.Table("senses").
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
func (r *Repository) GetPronunciationsByWordID(ctx context.Context, wordID uint, accent *int) ([]Pronunciation, error) {
	var pronunciations []Pronunciation
	query := r.db.WithContext(ctx).Where("word_id = ?", wordID)

	if accent != nil {
		query = query.Where("accent = ?", *accent)
	}

	if err := query.Find(&pronunciations).Error; err != nil {
		return nil, err
	}

	return pronunciations, nil
}

// GetSensesByWordID retrieves senses for a word
func (r *Repository) GetSensesByWordID(ctx context.Context, wordID uint, pos *int) ([]Sense, error) {
	var senses []Sense
	query := r.db.WithContext(ctx).Where("word_id = ?", wordID)

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
