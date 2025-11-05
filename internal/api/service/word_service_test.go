package service

import (
	"errors"
	"testing"

	"github.com/lib/pq"
	"github.com/simp-lee/isdict-api/internal/api/repository"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/model"
)

// mockRepository is a mock implementation of the WordRepository interface
type mockRepository struct {
	getWordByHeadwordFunc         func(headword string, includeVariants, includePronunciations, includeSenses bool) (*repository.Word, *repository.WordVariant, error)
	getWordsByHeadwordsFunc       func(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error)
	getWordsByVariantFunc         func(variant string, kind *int) ([]repository.Word, []repository.WordVariant, error)
	searchWordsFunc               func(keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]repository.Word, int64, error)
	suggestWordsFunc              func(prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]repository.Word, error)
	searchPhrasesFunc             func(keyword string, limit int) ([]repository.Word, error)
	getPronunciationsByWordIDFunc func(wordID uint, accent *int) ([]repository.Pronunciation, error)
	getSensesByWordIDFunc         func(wordID uint, pos *int) ([]repository.Sense, error)
}

func (m *mockRepository) GetWordByHeadword(headword string, includeVariants, includePronunciations, includeSenses bool) (*repository.Word, *repository.WordVariant, error) {
	if m.getWordByHeadwordFunc != nil {
		return m.getWordByHeadwordFunc(headword, includeVariants, includePronunciations, includeSenses)
	}
	return nil, nil, repository.ErrWordNotFound
}

func (m *mockRepository) GetWordsByHeadwords(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
	if m.getWordsByHeadwordsFunc != nil {
		return m.getWordsByHeadwordsFunc(headwords, includeVariants, includePronunciations, includeSenses)
	}
	return []repository.Word{}, nil
}

func (m *mockRepository) GetWordsByVariant(variant string, kind *int) ([]repository.Word, []repository.WordVariant, error) {
	if m.getWordsByVariantFunc != nil {
		return m.getWordsByVariantFunc(variant, kind)
	}
	return nil, nil, repository.ErrVariantNotFound
}

func (m *mockRepository) SearchWords(keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]repository.Word, int64, error) {
	if m.searchWordsFunc != nil {
		return m.searchWordsFunc(keyword, pos, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit, offset)
	}
	return []repository.Word{}, 0, nil
}

func (m *mockRepository) SuggestWords(prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]repository.Word, error) {
	if m.suggestWordsFunc != nil {
		return m.suggestWordsFunc(prefix, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit)
	}
	return []repository.Word{}, nil
}

func (m *mockRepository) SearchPhrases(keyword string, limit int) ([]repository.Word, error) {
	if m.searchPhrasesFunc != nil {
		return m.searchPhrasesFunc(keyword, limit)
	}
	return []repository.Word{}, nil
}

func (m *mockRepository) GetPronunciationsByWordID(wordID uint, accent *int) ([]repository.Pronunciation, error) {
	if m.getPronunciationsByWordIDFunc != nil {
		return m.getPronunciationsByWordIDFunc(wordID, accent)
	}
	return []repository.Pronunciation{}, nil
}

func (m *mockRepository) GetSensesByWordID(wordID uint, pos *int) ([]repository.Sense, error) {
	if m.getSensesByWordIDFunc != nil {
		return m.getSensesByWordIDFunc(wordID, pos)
	}
	return []repository.Sense{}, nil
}

func createTestConfig() *config.Config {
	return &config.Config{
		APIBatchMaxSize:    100,
		APISearchMaxLimit:  100,
		APISuggestMaxLimit: 50,
	}
}

func TestGetWordsBatch_LimitExceeded(t *testing.T) {
	cfg := createTestConfig()
	mockRepo := &mockRepository{}
	service := NewWordService(mockRepo, cfg)

	// Create request with more than max size
	words := make([]string, 101)
	for i := range words {
		words[i] = "test"
	}

	req := &model.BatchRequest{
		Words: words,
	}

	_, _, err := service.GetWordsBatch(req)
	if err == nil {
		t.Fatal("Expected error for batch limit exceeded, got nil")
	}

	if !errors.Is(err, ErrBatchLimitExceeded) {
		t.Errorf("Expected ErrBatchLimitExceeded, got %v", err)
	}
}

func TestGetWordsBatch_EmptyRequest(t *testing.T) {
	cfg := createTestConfig()
	mockRepo := &mockRepository{}
	service := NewWordService(mockRepo, cfg)

	req := &model.BatchRequest{
		Words: []string{},
	}

	responses, meta, err := service.GetWordsBatch(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(responses) != 0 {
		t.Errorf("Expected empty responses, got %d", len(responses))
	}

	if meta != nil {
		t.Error("Expected nil meta for empty request")
	}
}

func TestGetWordsBatch_PreservesOrder(t *testing.T) {
	cfg := createTestConfig()

	// Mock repository returns words in different order
	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			return []repository.Word{
				{ID: 3, Headword: "cat"},
				{ID: 1, Headword: "apple"},
				{ID: 2, Headword: "book"},
			}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	req := &model.BatchRequest{
		Words: []string{"apple", "book", "cat"},
	}

	responses, meta, err := service.GetWordsBatch(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(responses) != 3 {
		t.Fatalf("Expected 3 responses, got %d", len(responses))
	}

	// Check order is preserved
	expectedOrder := []string{"apple", "book", "cat"}
	for i, resp := range responses {
		if resp.Headword != expectedOrder[i] {
			t.Errorf("Expected word at position %d to be %s, got %s", i, expectedOrder[i], resp.Headword)
		}
	}

	// Check meta info
	if meta == nil {
		t.Fatal("Expected meta info, got nil")
	}
	if *meta.Requested != 3 {
		t.Errorf("Expected requested=3, got %d", *meta.Requested)
	}
	if *meta.Found != 3 {
		t.Errorf("Expected found=3, got %d", *meta.Found)
	}
	if len(meta.NotFound) != 0 {
		t.Errorf("Expected no not found words, got %v", meta.NotFound)
	}
}

func TestGetWordsBatch_CaseSensitiveVariants(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			return []repository.Word{
				{ID: 1, Headword: "Polish"},
				{ID: 2, Headword: "polish"},
			}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	req := &model.BatchRequest{
		Words: []string{"Polish", "polish"},
	}

	responses, meta, err := service.GetWordsBatch(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(responses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(responses))
	}

	if responses[0].Headword != "Polish" {
		t.Fatalf("Expected first response to be 'Polish', got %s", responses[0].Headword)
	}

	if responses[1].Headword != "polish" {
		t.Fatalf("Expected second response to be 'polish', got %s", responses[1].Headword)
	}

	if meta == nil {
		t.Fatal("Expected meta info, got nil")
	}

	if *meta.Requested != 2 {
		t.Errorf("Expected requested=2, got %d", *meta.Requested)
	}

	if *meta.Found != 2 {
		t.Errorf("Expected found=2, got %d", *meta.Found)
	}

	if len(meta.NotFound) != 0 {
		t.Errorf("Expected no not found words, got %v", meta.NotFound)
	}
}

func TestGetWordsBatch_PartialResults(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			return []repository.Word{
				{ID: 1, Headword: "apple"},
			}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	req := &model.BatchRequest{
		Words: []string{"apple", "xyz123", "book"},
	}

	responses, meta, err := service.GetWordsBatch(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("Expected 1 response, got %d", len(responses))
	}

	if meta == nil {
		t.Fatal("Expected meta info, got nil")
	}
	if *meta.Requested != 3 {
		t.Errorf("Expected requested=3, got %d", *meta.Requested)
	}
	if *meta.Found != 1 {
		t.Errorf("Expected found=1, got %d", *meta.Found)
	}
	if len(meta.NotFound) != 2 {
		t.Errorf("Expected 2 not found words, got %v", meta.NotFound)
	}
}

func TestSearchWords_LimitValidation(t *testing.T) {
	cfg := createTestConfig()

	callCount := 0
	expectedLimits := []int{100, 20, 20} // max for >max, default for <=0, default for negative

	mockRepo := &mockRepository{
		searchWordsFunc: func(keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]repository.Word, int64, error) {
			if callCount < len(expectedLimits) {
				if limit != expectedLimits[callCount] {
					t.Errorf("Call %d: Expected limit to be %d, got %d", callCount, expectedLimits[callCount], limit)
				}
			}
			callCount++
			return []repository.Word{}, 0, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	// Test with limit > max (should use max)
	_, _, err := service.SearchWords("test", nil, nil, nil, nil, nil, nil, 101, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Test with limit <= 0 (should use default)
	_, _, err = service.SearchWords("test", nil, nil, nil, nil, nil, nil, 0, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Test with negative limit (should use default)
	_, _, err = service.SearchWords("test", nil, nil, nil, nil, nil, nil, -1, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestSearchWords_OffsetValidation(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		searchWordsFunc: func(keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]repository.Word, int64, error) {
			if offset != 0 { // Should be reset to 0
				t.Errorf("Expected offset to be reset to 0, got %d", offset)
			}
			return []repository.Word{}, 0, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	// Test with negative offset
	_, _, err := service.SearchWords("test", nil, nil, nil, nil, nil, nil, 20, -10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestSuggestWords_LimitValidation(t *testing.T) {
	cfg := createTestConfig()

	callCount := 0
	expectedLimits := []int{50, 10, 10} // max for >max, default for <=0, default for negative

	mockRepo := &mockRepository{
		suggestWordsFunc: func(prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]repository.Word, error) {
			if callCount < len(expectedLimits) {
				if limit != expectedLimits[callCount] {
					t.Errorf("Call %d: Expected limit to be %d, got %d", callCount, expectedLimits[callCount], limit)
				}
			}
			callCount++
			return []repository.Word{}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	// Test with limit > max (should use max)
	_, err := service.SuggestWords("test", nil, nil, nil, nil, nil, 51)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Test with limit <= 0 (should use default)
	_, err = service.SuggestWords("test", nil, nil, nil, nil, nil, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestGetWordsByVariant_ValidKind(t *testing.T) {
	cfg := createTestConfig()

	formKind := int(model.VariantForm)
	mockRepo := &mockRepository{
		getWordsByVariantFunc: func(variant string, kind *int) ([]repository.Word, []repository.WordVariant, error) {
			if kind == nil {
				t.Error("Expected kind to be set, got nil")
			} else if *kind != formKind {
				t.Errorf("Expected kind=%d, got %d", formKind, *kind)
			}
			return []repository.Word{
					{ID: 1, Headword: "test"},
				}, []repository.WordVariant{
					{WordID: 1, VariantText: "testing"},
				}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	kindStr := "form"
	results, err := service.GetWordsByVariant("testing", &kindStr, true, true)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
}

func TestGetWordByHeadword_NotFound(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordByHeadwordFunc: func(headword string, includeVariants, includePronunciations, includeSenses bool) (*repository.Word, *repository.WordVariant, error) {
			return nil, nil, repository.ErrWordNotFound
		},
	}

	service := NewWordService(mockRepo, cfg)

	_, err := service.GetWordByHeadword("nonexistent", nil, true, true, true)
	if err == nil {
		t.Fatal("Expected error for word not found, got nil")
	}

	if !errors.Is(err, repository.ErrWordNotFound) {
		t.Errorf("Expected ErrWordNotFound, got %v", err)
	}
}

func TestGetWordsByVariant_MultipleForms(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByVariantFunc: func(variant string, kind *int) ([]repository.Word, []repository.WordVariant, error) {
			if variant != "lit" {
				t.Fatalf("unexpected variant: %s", variant)
			}

			words := []repository.Word{
				{
					ID:            1,
					Headword:      "light",
					CEFRLevel:     1,
					FrequencyRank: 150,
				},
			}

			variants := []repository.WordVariant{
				{
					WordID:      1,
					VariantText: "lit",
					Kind:        model.VariantForm,
					FormType:    intPtr(1),
					Tags:        pq.StringArray{"past"},
				},
				{
					WordID:      1,
					VariantText: "lit",
					Kind:        model.VariantForm,
					FormType:    intPtr(2),
					Tags:        pq.StringArray{"past_participle"},
				},
			}

			return words, variants, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	results, err := service.GetWordsByVariant("lit", nil, true, true)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	info := results[0].VariantInfo
	if len(info) != 2 {
		t.Fatalf("Expected 2 variant entries, got %d", len(info))
	}

	if info[0].FormType != "past" || info[0].Tags[0] != "past" {
		t.Errorf("Unexpected first variant: %+v", info[0])
	}
	if info[1].FormType != "past_participle" || info[1].Tags[0] != "past_participle" {
		t.Errorf("Unexpected second variant: %+v", info[1])
	}
}

func TestGetWordsByVariant_TagsRemainNormalized(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByVariantFunc: func(variant string, kind *int) ([]repository.Word, []repository.WordVariant, error) {
			words := []repository.Word{
				{
					ID:            10,
					Headword:      "color",
					CEFRLevel:     2,
					FrequencyRank: 450,
				},
			}

			variants := []repository.WordVariant{
				{
					WordID:      10,
					VariantText: "colour",
					Kind:        model.VariantAlias,
					Tags:        pq.StringArray{"british", "alternative_spelling"},
				},
			}

			return words, variants, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	results, err := service.GetWordsByVariant("colour", nil, true, true)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	tags := results[0].VariantInfo[0].Tags
	if len(tags) != 2 {
		t.Fatalf("Expected 2 tags, got %d", len(tags))
	}

	if tags[0] != "british" {
		t.Errorf("Expected first tag 'british', got %q", tags[0])
	}
	if tags[1] != "alternative_spelling" {
		t.Errorf("Expected second tag 'alternative_spelling', got %q", tags[1])
	}
}

func intPtr(v int) *int {
	return &v
}

// TestGetWordsBatch_WithVariantFallback tests the automatic fallback to variant query
// when a word is not found in the main words table
func TestGetWordsBatch_WithVariantFallback(t *testing.T) {
	cfg := createTestConfig()

	// Track which queries were made
	getWordsByHeadwordsCalled := false
	getWordByHeadwordCalls := []string{}

	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			getWordsByHeadwordsCalled = true
			// Only return "apple", "book" is not found in main table
			return []repository.Word{
				{ID: 1, Headword: "apple"},
			}, nil
		},
		getWordByHeadwordFunc: func(headword string, includeVariants, includePronunciations, includeSenses bool) (*repository.Word, *repository.WordVariant, error) {
			getWordByHeadwordCalls = append(getWordByHeadwordCalls, headword)
			// Simulate variant fallback: "book" found via variant, "xyz" not found at all
			if headword == "book" {
				// Return word found via variant with the variant info
				return &repository.Word{ID: 2, Headword: "book"},
					&repository.WordVariant{WordID: 2, VariantText: headword},
					nil
			}
			return nil, nil, repository.ErrWordNotFound
		},
	}

	service := NewWordService(mockRepo, cfg)

	req := &model.BatchRequest{
		Words: []string{"apple", "book", "xyz"},
	}

	responses, meta, err := service.GetWordsBatch(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify batch query was called
	if !getWordsByHeadwordsCalled {
		t.Error("Expected GetWordsByHeadwords to be called")
	}

	// Verify fallback was triggered for "book" and "xyz" (not found in main query)
	if len(getWordByHeadwordCalls) != 2 {
		t.Errorf("Expected 2 fallback queries, got %d: %v", len(getWordByHeadwordCalls), getWordByHeadwordCalls)
	}

	// Verify results: 2 found (apple + book via fallback), 1 not found (xyz)
	if len(responses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(responses))
	}

	if meta == nil {
		t.Fatal("Expected meta info, got nil")
	}
	if *meta.Requested != 3 {
		t.Errorf("Expected requested=3, got %d", *meta.Requested)
	}
	if *meta.Found != 2 {
		t.Errorf("Expected found=2, got %d", *meta.Found)
	}
	if len(meta.NotFound) != 1 {
		t.Errorf("Expected 1 not found word, got %v", meta.NotFound)
	}
	if meta.NotFound[0] != "xyz" {
		t.Errorf("Expected 'xyz' in not found, got %v", meta.NotFound)
	}

	// Verify order is preserved (apple, book)
	if responses[0].Headword != "apple" {
		t.Errorf("Expected first response to be 'apple', got %s", responses[0].Headword)
	}
	if responses[1].Headword != "book" {
		t.Errorf("Expected second response to be 'book', got %s", responses[1].Headword)
	}
}

// TestGetWordsBatch_NormalizedFormMatching tests that batch query correctly uses
// normalized form for matching (e.g., "air-conditioning" matches "air conditioning")
func TestGetWordsBatch_NormalizedFormMatching(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			// Simulate database returns normalized form: "air conditioning" (with space)
			return []repository.Word{
				{ID: 1, Headword: "air conditioning"},
			}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	// User queries with hyphen: "air-conditioning"
	req := &model.BatchRequest{
		Words: []string{"air-conditioning"},
	}

	responses, meta, err := service.GetWordsBatch(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should find the word despite different spelling
	if len(responses) != 1 {
		t.Fatalf("Expected 1 response, got %d", len(responses))
	}

	if responses[0].Headword != "air conditioning" {
		t.Errorf("Expected 'air conditioning', got %s", responses[0].Headword)
	}

	if meta == nil || *meta.Found != 1 {
		t.Error("Expected to find 1 word via normalized matching")
	}
}

// TestGetWordsBatch_ApostrophePreservation tests that apostrophes are preserved
// in normalized form, distinguishing "it's" from "its"
func TestGetWordsBatch_ApostrophePreservation(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			// Return both words
			return []repository.Word{
				{ID: 1, Headword: "it's"},
				{ID: 2, Headword: "its"},
			}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	req := &model.BatchRequest{
		Words: []string{"it's", "its"},
	}

	responses, meta, err := service.GetWordsBatch(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should find both words as distinct entries
	if len(responses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(responses))
	}

	if responses[0].Headword != "it's" {
		t.Errorf("Expected first response 'it's', got %s", responses[0].Headword)
	}
	if responses[1].Headword != "its" {
		t.Errorf("Expected second response 'its', got %s", responses[1].Headword)
	}

	if meta == nil || *meta.Found != 2 {
		t.Error("Expected to find 2 distinct words")
	}
}

// TestSearchWords_KeywordLengthValidation tests input length validation
func TestSearchWords_KeywordLengthValidation(t *testing.T) {
	cfg := createTestConfig()
	mockRepo := &mockRepository{}
	service := NewWordService(mockRepo, cfg)

	tests := []struct {
		name        string
		keyword     string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid keyword",
			keyword:     "test",
			expectError: false,
		},
		{
			name:        "keyword too short (1 char)",
			keyword:     "a",
			expectError: true,
			errorMsg:    "at least 2 characters",
		},
		{
			name:        "keyword too long (>100 chars)",
			keyword:     string(make([]rune, 101)),
			expectError: true,
			errorMsg:    "not exceed 100 characters",
		},
		{
			name:        "minimum valid length (2 chars)",
			keyword:     "ab",
			expectError: false,
		},
		{
			name:        "maximum valid length (100 chars)",
			keyword:     string(make([]rune, 100)),
			expectError: false,
		},
		{
			name:        "unicode characters",
			keyword:     "����",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := service.SearchWords(tt.keyword, nil, nil, nil, nil, nil, nil, 10, 0)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for keyword %q, got nil", tt.keyword)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for keyword %q, got %v", tt.keyword, err)
				}
			}
		})
	}
}

// TestSuggestWords_PrefixLengthValidation tests prefix length validation
func TestSuggestWords_PrefixLengthValidation(t *testing.T) {
	cfg := createTestConfig()
	mockRepo := &mockRepository{}
	service := NewWordService(mockRepo, cfg)

	tests := []struct {
		name        string
		prefix      string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid prefix",
			prefix:      "test",
			expectError: false,
		},
		{
			name:        "prefix too short (0 chars)",
			prefix:      "",
			expectError: true,
			errorMsg:    "at least 1 character",
		},
		{
			name:        "prefix too long (>50 chars)",
			prefix:      string(make([]rune, 51)),
			expectError: true,
			errorMsg:    "not exceed 50 characters",
		},
		{
			name:        "minimum valid length (1 char)",
			prefix:      "a",
			expectError: false,
		},
		{
			name:        "maximum valid length (50 chars)",
			prefix:      string(make([]rune, 50)),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.SuggestWords(tt.prefix, nil, nil, nil, nil, nil, 10)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for prefix %q, got nil", tt.prefix)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for prefix %q, got %v", tt.prefix, err)
				}
			}
		})
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestGetWordsBatch_MixedScenario tests a realistic scenario with:
// - Direct matches in main table
// - Variant fallback matches
// - Not found words
// - Different spellings (normalized form matching)
func TestGetWordsBatch_MixedScenario(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			// Main table returns: "apple", "air conditioning"
			return []repository.Word{
				{ID: 1, Headword: "apple"},
				{ID: 2, Headword: "air conditioning"},
			}, nil
		},
		getWordByHeadwordFunc: func(headword string, includeVariants, includePronunciations, includeSenses bool) (*repository.Word, *repository.WordVariant, error) {
			// Fallback: "lit" found via variant (maps to "light")
			if headword == "lit" {
				return &repository.Word{ID: 3, Headword: "light"},
					&repository.WordVariant{WordID: 3, VariantText: headword},
					nil
			}
			// "nonexistent" not found anywhere
			return nil, nil, repository.ErrWordNotFound
		},
	}

	service := NewWordService(mockRepo, cfg)

	req := &model.BatchRequest{
		Words: []string{"apple", "air-conditioning", "lit", "nonexistent"},
	}

	responses, meta, err := service.GetWordsBatch(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Expected results:
	// 1. "apple" - direct match
	// 2. "air-conditioning" - matched "air conditioning" via normalized form
	// 3. "lit" - found "light" via variant fallback
	// 4. "nonexistent" - not found

	if len(responses) != 3 {
		t.Fatalf("Expected 3 responses, got %d", len(responses))
	}

	expectedHeadwords := []string{"apple", "air conditioning", "light"}
	for i, expected := range expectedHeadwords {
		if responses[i].Headword != expected {
			t.Errorf("Response[%d]: expected %s, got %s", i, expected, responses[i].Headword)
		}
	}

	if meta == nil {
		t.Fatal("Expected meta info, got nil")
	}
	if *meta.Requested != 4 {
		t.Errorf("Expected requested=4, got %d", *meta.Requested)
	}
	if *meta.Found != 3 {
		t.Errorf("Expected found=3, got %d", *meta.Found)
	}
	if len(meta.NotFound) != 1 || meta.NotFound[0] != "nonexistent" {
		t.Errorf("Expected not found=['nonexistent'], got %v", meta.NotFound)
	}
}
