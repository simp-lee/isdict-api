package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/lib/pq"
	"github.com/simp-lee/isdict-api/internal/api/repository"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/model"
)

// mockRepository is a mock implementation of the WordRepository interface
type mockRepository struct {
	getWordByHeadwordFunc         func(ctx context.Context, headword string, includeVariants, includePronunciations, includeSenses bool) (*model.Word, *model.WordVariant, error)
	getWordsByHeadwordsFunc       func(ctx context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]model.Word, error)
	getWordsByVariantsFunc        func(ctx context.Context, variants []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.BatchVariantMatch, error)
	getWordsByVariantFunc         func(ctx context.Context, variant string, kind *int, includePronunciations, includeSenses bool) ([]model.Word, []model.WordVariant, error)
	searchWordsFunc               func(ctx context.Context, keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.Word, int64, error)
	suggestWordsFunc              func(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.Word, error)
	searchPhrasesFunc             func(ctx context.Context, keyword string, limit int) ([]model.Word, error)
	getPronunciationsByWordIDFunc func(ctx context.Context, wordID uint, accent *int) ([]model.Pronunciation, error)
	getSensesByWordIDFunc         func(ctx context.Context, wordID uint, pos *int) ([]model.Sense, error)
}

func (m *mockRepository) GetWordByHeadword(ctx context.Context, headword string, includeVariants, includePronunciations, includeSenses bool) (*model.Word, *model.WordVariant, error) {
	if m.getWordByHeadwordFunc != nil {
		return m.getWordByHeadwordFunc(ctx, headword, includeVariants, includePronunciations, includeSenses)
	}
	return nil, nil, repository.ErrWordNotFound
}

func (m *mockRepository) GetWordsByHeadwords(ctx context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]model.Word, error) {
	if m.getWordsByHeadwordsFunc != nil {
		return m.getWordsByHeadwordsFunc(ctx, headwords, includeVariants, includePronunciations, includeSenses)
	}
	return []model.Word{}, nil
}

func (m *mockRepository) GetWordsByVariants(ctx context.Context, variants []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.BatchVariantMatch, error) {
	if m.getWordsByVariantsFunc != nil {
		return m.getWordsByVariantsFunc(ctx, variants, includeVariants, includePronunciations, includeSenses)
	}
	return []repository.BatchVariantMatch{}, nil
}

func (m *mockRepository) GetWordsByVariant(ctx context.Context, variant string, kind *int, includePronunciations, includeSenses bool) ([]model.Word, []model.WordVariant, error) {
	if m.getWordsByVariantFunc != nil {
		return m.getWordsByVariantFunc(ctx, variant, kind, includePronunciations, includeSenses)
	}
	return nil, nil, repository.ErrVariantNotFound
}

func (m *mockRepository) SearchWords(ctx context.Context, keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.Word, int64, error) {
	if m.searchWordsFunc != nil {
		return m.searchWordsFunc(ctx, keyword, pos, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit, offset)
	}
	return []model.Word{}, 0, nil
}

func (m *mockRepository) SuggestWords(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.Word, error) {
	if m.suggestWordsFunc != nil {
		return m.suggestWordsFunc(ctx, prefix, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit)
	}
	return []model.Word{}, nil
}

func (m *mockRepository) SearchPhrases(ctx context.Context, keyword string, limit int) ([]model.Word, error) {
	if m.searchPhrasesFunc != nil {
		return m.searchPhrasesFunc(ctx, keyword, limit)
	}
	return []model.Word{}, nil
}

func (m *mockRepository) GetPronunciationsByWordID(ctx context.Context, wordID uint, accent *int) ([]model.Pronunciation, error) {
	if m.getPronunciationsByWordIDFunc != nil {
		return m.getPronunciationsByWordIDFunc(ctx, wordID, accent)
	}
	return []model.Pronunciation{}, nil
}

func (m *mockRepository) GetSensesByWordID(ctx context.Context, wordID uint, pos *int) ([]model.Sense, error) {
	if m.getSensesByWordIDFunc != nil {
		return m.getSensesByWordIDFunc(ctx, wordID, pos)
	}
	return []model.Sense{}, nil
}

func createTestConfig() *config.Config {
	return &config.Config{
		APIBatchMaxSize:    100,
		APISearchMaxLimit:  100,
		APISuggestMaxLimit: 50,
	}
}

type testContextKey string

func TestWordService_ForwardsContextToRepository(t *testing.T) {
	ctx := context.WithValue(context.Background(), testContextKey("request-id"), "req-123")
	cfg := createTestConfig()

	tests := []struct {
		name string
		run  func(*testing.T, context.Context, *config.Config)
	}{
		{name: "GetWordByHeadword", run: runContextForwardingGetWordByHeadword},
		{name: "GetWordsByVariant", run: runContextForwardingGetWordsByVariant},
		{name: "GetWordsBatch", run: runContextForwardingGetWordsBatch},
		{name: "SearchWords", run: runContextForwardingSearchWords},
		{name: "SuggestWords", run: runContextForwardingSuggestWords},
		{name: "SearchPhrases", run: runContextForwardingSearchPhrases},
		{name: "GetPronunciations", run: runContextForwardingGetPronunciations},
		{name: "GetSenses", run: runContextForwardingGetSenses},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, ctx, cfg)
		})
	}
}

func runContextForwardingGetWordByHeadword(t *testing.T, ctx context.Context, cfg *config.Config) {
	service := NewWordService(&mockRepository{
		getWordByHeadwordFunc: func(callCtx context.Context, headword string, includeVariants, includePronunciations, includeSenses bool) (*repository.Word, *repository.WordVariant, error) {
			assertForwardedContext(t, ctx, callCtx)
			return &repository.Word{Headword: headword}, nil, nil
		},
	}, cfg)
	_, err := service.GetWordByHeadword(ctx, "learn", nil, false, false, false)
	assertNoServiceError(t, err)
}

func runContextForwardingGetWordsByVariant(t *testing.T, ctx context.Context, cfg *config.Config) {
	service := NewWordService(&mockRepository{
		getWordsByVariantFunc: func(callCtx context.Context, variant string, kind *int, includePronunciations, includeSenses bool) ([]repository.Word, []repository.WordVariant, error) {
			assertForwardedContext(t, ctx, callCtx)
			if includePronunciations || includeSenses {
				t.Fatalf("expected include flags to stay false, got pronunciations=%v senses=%v", includePronunciations, includeSenses)
			}
			return []repository.Word{{ID: 1, Headword: "learn"}}, []repository.WordVariant{{WordID: 1, VariantText: variant}}, nil
		},
	}, cfg)
	_, err := service.GetWordsByVariant(ctx, "learnt", nil, false, false)
	assertNoServiceError(t, err)
}

func runContextForwardingGetWordsBatch(t *testing.T, ctx context.Context, cfg *config.Config) {
	batchFallbackCalled := false
	service := NewWordService(&mockRepository{
		getWordsByHeadwordsFunc: func(callCtx context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			assertForwardedContext(t, ctx, callCtx)
			if len(headwords) != 2 {
				t.Fatalf("expected 2 headwords, got %d", len(headwords))
			}
			return []repository.Word{{ID: 1, Headword: headwords[0]}}, nil
		},
		getWordsByVariantsFunc: func(callCtx context.Context, variants []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.BatchVariantMatch, error) {
			assertForwardedContext(t, ctx, callCtx)
			batchFallbackCalled = true
			if !reflect.DeepEqual(variants, []string{"learnt"}) {
				t.Fatalf("expected batch fallback for [learnt], got %v", variants)
			}
			return []repository.BatchVariantMatch{{
				Word:    repository.Word{ID: 2, Headword: "learn"},
				Variant: repository.WordVariant{WordID: 2, VariantText: "learnt"},
			}}, nil
		},
	}, cfg)
	responses, meta, err := service.GetWordsBatch(ctx, &model.BatchRequest{Words: []string{"learn", "learnt"}})
	assertNoServiceError(t, err)
	if !batchFallbackCalled {
		t.Fatal("expected batch fallback repository call")
	}
	if len(responses) != 2 || meta == nil || *meta.Found != 2 {
		t.Fatalf("unexpected batch result: responses=%d meta=%+v", len(responses), meta)
	}
}

func runContextForwardingSearchWords(t *testing.T, ctx context.Context, cfg *config.Config) {
	service := NewWordService(&mockRepository{
		searchWordsFunc: func(callCtx context.Context, keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]repository.Word, int64, error) {
			assertForwardedContext(t, ctx, callCtx)
			return nil, 0, nil
		},
	}, cfg)
	_, _, err := service.SearchWords(ctx, "learn", nil, nil, nil, nil, nil, nil, 20, 0)
	assertNoServiceError(t, err)
}

func runContextForwardingSuggestWords(t *testing.T, ctx context.Context, cfg *config.Config) {
	service := NewWordService(&mockRepository{
		suggestWordsFunc: func(callCtx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]repository.Word, error) {
			assertForwardedContext(t, ctx, callCtx)
			return nil, nil
		},
	}, cfg)
	_, err := service.SuggestWords(ctx, "lea", nil, nil, nil, nil, nil, 10)
	assertNoServiceError(t, err)
}

func runContextForwardingSearchPhrases(t *testing.T, ctx context.Context, cfg *config.Config) {
	service := NewWordService(&mockRepository{
		searchPhrasesFunc: func(callCtx context.Context, keyword string, limit int) ([]repository.Word, error) {
			assertForwardedContext(t, ctx, callCtx)
			return nil, nil
		},
	}, cfg)
	_, err := service.SearchPhrases(ctx, "look", 10)
	assertNoServiceError(t, err)
}

func runContextForwardingGetPronunciations(t *testing.T, ctx context.Context, cfg *config.Config) {
	service := NewWordService(&mockRepository{
		getWordByHeadwordFunc: func(callCtx context.Context, headword string, includeVariants, includePronunciations, includeSenses bool) (*repository.Word, *repository.WordVariant, error) {
			assertForwardedContext(t, ctx, callCtx)
			return &repository.Word{ID: 1, Headword: headword}, nil, nil
		},
		getPronunciationsByWordIDFunc: func(callCtx context.Context, wordID uint, accent *int) ([]model.Pronunciation, error) {
			assertForwardedContext(t, ctx, callCtx)
			return nil, nil
		},
	}, cfg)
	_, err := service.GetPronunciations(ctx, "learn", nil)
	assertNoServiceError(t, err)
}

func runContextForwardingGetSenses(t *testing.T, ctx context.Context, cfg *config.Config) {
	service := NewWordService(&mockRepository{
		getWordByHeadwordFunc: func(callCtx context.Context, headword string, includeVariants, includePronunciations, includeSenses bool) (*repository.Word, *repository.WordVariant, error) {
			assertForwardedContext(t, ctx, callCtx)
			return &repository.Word{ID: 1, Headword: headword}, nil, nil
		},
		getSensesByWordIDFunc: func(callCtx context.Context, wordID uint, pos *int) ([]model.Sense, error) {
			assertForwardedContext(t, ctx, callCtx)
			return nil, nil
		},
	}, cfg)
	_, err := service.GetSenses(ctx, "learn", nil, langBoth)
	assertNoServiceError(t, err)
}

func assertForwardedContext(t *testing.T, want, got context.Context) {
	t.Helper()
	if got != want {
		t.Fatalf("expected forwarded context")
	}
}

func assertNoServiceError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestGetWordsBatch_LimitExceeded(t *testing.T) {
	cfg := createTestConfig()
	mockRepo := &mockRepository{}
	service := NewWordService(mockRepo, cfg)

	// Create request with more than max size
	words := make([]string, 101)
	for i := range words {
		words[i] = fmt.Sprintf("test-%d", i)
	}

	req := &model.BatchRequest{
		Words: words,
	}

	_, _, err := service.GetWordsBatch(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error for batch limit exceeded, got nil")
	}

	if !errors.Is(err, ErrBatchLimitExceeded) {
		t.Errorf("Expected ErrBatchLimitExceeded, got %v", err)
	}
}

func TestGetWordsBatch_AllowsRawOversizedInputWhenCleanedBatchFitsLimit(t *testing.T) {
	cfg := createTestConfig()
	cfg.APIBatchMaxSize = 2

	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(_ context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]model.Word, error) {
			expected := []string{"apple", "pear"}
			if !reflect.DeepEqual(headwords, expected) {
				t.Fatalf("expected cleaned headwords %v, got %v", expected, headwords)
			}
			return []model.Word{
				{ID: 1, Headword: "apple"},
				{ID: 2, Headword: "pear"},
			}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)
	responses, meta, err := service.GetWordsBatch(context.Background(), &model.BatchRequest{
		Words: []string{" apple ", "", "apple", "pear", "  ", "pear"},
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(responses))
	}
	if meta == nil {
		t.Fatal("Expected meta info, got nil")
	}
	if *meta.Requested != 2 {
		t.Fatalf("Expected requested=2 after cleanup, got %d", *meta.Requested)
	}
	if *meta.Found != 2 {
		t.Fatalf("Expected found=2, got %d", *meta.Found)
	}
	if len(meta.NotFound) != 0 {
		t.Fatalf("Expected no not_found entries, got %v", meta.NotFound)
	}
}

func TestGetWordsBatch_PreservesSeparatorDistinctInputs(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(_ context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]model.Word, error) {
			expected := []string{"cooperate", "co-operate"}
			if !reflect.DeepEqual(headwords, expected) {
				t.Fatalf("expected cleaned headwords %v, got %v", expected, headwords)
			}
			return []model.Word{{ID: 1, Headword: "cooperate"}, {ID: 2, Headword: "co-operate"}}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)
	responses, meta, err := service.GetWordsBatch(context.Background(), &model.BatchRequest{
		Words: []string{"cooperate", "co-operate", " cooperate "},
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(responses))
	}
	if responses[0].Headword != "cooperate" {
		t.Fatalf("Expected cooperate response, got %q", responses[0].Headword)
	}
	if responses[1].Headword != "co-operate" {
		t.Fatalf("Expected co-operate response, got %q", responses[1].Headword)
	}
	if meta == nil {
		t.Fatal("Expected meta info, got nil")
	}
	if *meta.Requested != 2 {
		t.Fatalf("Expected requested=2 after exact-only dedupe, got %d", *meta.Requested)
	}
	if *meta.Found != 2 {
		t.Fatalf("Expected found=2, got %d", *meta.Found)
	}
	if len(meta.NotFound) != 0 {
		t.Fatalf("Expected no not_found entries, got %v", meta.NotFound)
	}
}

func TestGetWordsBatch_EmptyRequest(t *testing.T) {
	cfg := createTestConfig()
	mockRepo := &mockRepository{}
	service := NewWordService(mockRepo, cfg)

	responses, meta, err := service.GetWordsBatch(context.Background(), &model.BatchRequest{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(responses) != 0 {
		t.Fatalf("Expected empty response slice, got %d items", len(responses))
	}
	if meta != nil {
		t.Fatalf("Expected nil meta for empty request, got %+v", meta)
	}
}

func TestGetWordsBatch_PreservesOrder(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(_ context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]model.Word, error) {
			return []model.Word{
				{ID: 3, Headword: "cat"},
				{ID: 1, Headword: "apple"},
				{ID: 2, Headword: "book"},
			}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	responses, meta, err := service.GetWordsBatch(context.Background(), &model.BatchRequest{
		Words: []string{"apple", "book", "cat"},
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(responses) != 3 {
		t.Fatalf("Expected 3 responses, got %d", len(responses))
	}

	expectedOrder := []string{"apple", "book", "cat"}
	for i, resp := range responses {
		if resp.Headword != expectedOrder[i] {
			t.Errorf("Expected word at position %d to be %s, got %s", i, expectedOrder[i], resp.Headword)
		}
	}
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
		getWordsByHeadwordsFunc: func(_ context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]model.Word, error) {
			return []model.Word{
				{ID: 1, Headword: "Polish"},
				{ID: 2, Headword: "polish"},
			}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	responses, meta, err := service.GetWordsBatch(context.Background(), &model.BatchRequest{
		Words: []string{"Polish", "polish"},
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(responses))
	}
	if responses[0].Headword != "Polish" {
		t.Fatalf("Expected first response Polish, got %q", responses[0].Headword)
	}
	if responses[1].Headword != "polish" {
		t.Fatalf("Expected second response polish, got %q", responses[1].Headword)
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
		getWordsByHeadwordsFunc: func(_ context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]model.Word, error) {
			return []model.Word{{ID: 1, Headword: "apple"}}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	responses, meta, err := service.GetWordsBatch(context.Background(), &model.BatchRequest{
		Words: []string{"apple", "xyz123", "book"},
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(responses) != 1 {
		t.Fatalf("Expected 1 response, got %d", len(responses))
	}
	if responses[0].Headword != "apple" {
		t.Fatalf("Expected apple response, got %q", responses[0].Headword)
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
	if !reflect.DeepEqual(meta.NotFound, []string{"xyz123", "book"}) {
		t.Errorf("Expected not found [xyz123 book], got %v", meta.NotFound)
	}
}

func TestSearchWords_LimitValidation(t *testing.T) {
	cfg := createTestConfig()

	callCount := 0
	expectedLimits := []int{100, 20, 20} // max for >max, default for <=0, default for negative

	mockRepo := &mockRepository{
		searchWordsFunc: func(_ context.Context, keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]repository.Word, int64, error) {
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
	_, _, err := service.SearchWords(context.Background(), "test", nil, nil, nil, nil, nil, nil, 101, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Test with limit <= 0 (should use default)
	_, _, err = service.SearchWords(context.Background(), "test", nil, nil, nil, nil, nil, nil, 0, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Test with negative limit (should use default)
	_, _, err = service.SearchWords(context.Background(), "test", nil, nil, nil, nil, nil, nil, -1, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestSearchWords_OffsetValidation(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		searchWordsFunc: func(_ context.Context, keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.Word, int64, error) {
			if offset != 0 {
				t.Errorf("Expected offset to be reset to 0, got %d", offset)
			}
			return []model.Word{}, 0, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	_, _, err := service.SearchWords(context.Background(), "test", nil, nil, nil, nil, nil, nil, 20, -10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestSuggestWords_LimitValidation(t *testing.T) {
	cfg := createTestConfig()

	callCount := 0
	expectedLimits := []int{50, 10, 10}

	mockRepo := &mockRepository{
		suggestWordsFunc: func(_ context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.Word, error) {
			if callCount < len(expectedLimits) && limit != expectedLimits[callCount] {
				t.Errorf("Call %d: Expected limit to be %d, got %d", callCount, expectedLimits[callCount], limit)
			}
			callCount++
			return []model.Word{}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	_, err := service.SuggestWords(context.Background(), "test", nil, nil, nil, nil, nil, 51)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	_, err = service.SuggestWords(context.Background(), "test", nil, nil, nil, nil, nil, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	_, err = service.SuggestWords(context.Background(), "test", nil, nil, nil, nil, nil, -1)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestGetWordsByVariant_ValidKind(t *testing.T) {
	cfg := createTestConfig()

	formKind := int(model.VariantForm)
	mockRepo := &mockRepository{
		getWordsByVariantFunc: func(_ context.Context, variant string, kind *int, includePronunciations, includeSenses bool) ([]model.Word, []model.WordVariant, error) {
			if kind == nil {
				t.Error("Expected kind to be set, got nil")
			} else if *kind != formKind {
				t.Errorf("Expected kind=%d, got %d", formKind, *kind)
			}
			if !includePronunciations || !includeSenses {
				t.Fatalf("expected include flags to be forwarded, got pronunciations=%v senses=%v", includePronunciations, includeSenses)
			}
			return []model.Word{{ID: 1, Headword: "test"}}, []model.WordVariant{{WordID: 1, VariantText: "testing"}}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	kindStr := "form"
	results, err := service.GetWordsByVariant(context.Background(), "testing", &kindStr, true, true)
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
		getWordByHeadwordFunc: func(_ context.Context, headword string, includeVariants, includePronunciations, includeSenses bool) (*model.Word, *model.WordVariant, error) {
			return nil, nil, repository.ErrWordNotFound
		},
	}

	service := NewWordService(mockRepo, cfg)

	_, err := service.GetWordByHeadword(context.Background(), "nonexistent", nil, true, true, true)
	if err == nil {
		t.Fatal("Expected error for word not found, got nil")
	}
	if !errors.Is(err, ErrWordNotFound) {
		t.Errorf("Expected ErrWordNotFound, got %v", err)
	}
}

func TestGetWordByHeadword_NotFoundWrappedError(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordByHeadwordFunc: func(_ context.Context, headword string, includeVariants, includePronunciations, includeSenses bool) (*model.Word, *model.WordVariant, error) {
			return nil, nil, fmt.Errorf("cache miss: %w", repository.ErrWordNotFound)
		},
	}

	service := NewWordService(mockRepo, cfg)

	_, err := service.GetWordByHeadword(context.Background(), "nonexistent", nil, true, true, true)
	if err == nil {
		t.Fatal("Expected error for word not found, got nil")
	}
	if !errors.Is(err, ErrWordNotFound) {
		t.Errorf("Expected ErrWordNotFound from wrapped repository error, got %v", err)
	}
}

func TestGetWordsByVariant_NotFoundWrappedError(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByVariantFunc: func(_ context.Context, variant string, kind *int, includePronunciations, includeSenses bool) ([]model.Word, []model.WordVariant, error) {
			return nil, nil, fmt.Errorf("decorator miss: %w", repository.ErrVariantNotFound)
		},
	}

	service := NewWordService(mockRepo, cfg)

	_, err := service.GetWordsByVariant(context.Background(), "nonexistent", nil, false, false)
	if err == nil {
		t.Fatal("Expected error for variant not found, got nil")
	}
	if !errors.Is(err, ErrVariantNotFound) {
		t.Errorf("Expected ErrVariantNotFound from wrapped repository error, got %v", err)
	}
}

func TestGetWordsByVariant_MultipleForms(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByVariantFunc: func(_ context.Context, variant string, kind *int, includePronunciations, includeSenses bool) ([]model.Word, []model.WordVariant, error) {
			if variant != "lit" {
				t.Fatalf("unexpected variant: %s", variant)
			}

			words := []model.Word{{
				ID:            1,
				Headword:      "light",
				CEFRLevel:     1,
				FrequencyRank: 150,
			}}

			variants := []model.WordVariant{
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

	results, err := service.GetWordsByVariant(context.Background(), "lit", nil, true, true)
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
	if info[0].FormType != "past" || len(info[0].Tags) != 1 || info[0].Tags[0] != "past" {
		t.Errorf("Unexpected first variant: %+v", info[0])
	}
	if info[1].FormType != "past_participle" || len(info[1].Tags) != 1 || info[1].Tags[0] != "past_participle" {
		t.Errorf("Unexpected second variant: %+v", info[1])
	}
}

func TestGetWordsByVariant_TagsRemainNormalized(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByVariantFunc: func(_ context.Context, variant string, kind *int, includePronunciations, includeSenses bool) ([]model.Word, []model.WordVariant, error) {
			words := []model.Word{{
				ID:            10,
				Headword:      "color",
				CEFRLevel:     2,
				FrequencyRank: 450,
			}}

			variants := []model.WordVariant{{
				WordID:      10,
				VariantText: "colour",
				Kind:        model.VariantAlias,
				Tags:        pq.StringArray{"british", "alternative_spelling"},
			}}

			return words, variants, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	results, err := service.GetWordsByVariant(context.Background(), "colour", nil, true, true)
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

func TestGetWordsByVariant_MapsCEFRLevel(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByVariantFunc: func(_ context.Context, variant string, kind *int, includePronunciations, includeSenses bool) ([]model.Word, []model.WordVariant, error) {
			return []model.Word{{
					ID:         1,
					Headword:   "light",
					CEFRLevel:  1,
					CEFRSource: "oxford",
				}}, []model.WordVariant{{
					WordID:      1,
					VariantText: "lit",
					Kind:        model.VariantForm,
				}}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	results, err := service.GetWordsByVariant(context.Background(), "lit", nil, false, false)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].CEFRLevel != "A1" {
		t.Fatalf("Expected CEFR level A1, got %q", results[0].CEFRLevel)
	}
	if results[0].CEFRSource != "oxford" {
		t.Fatalf("Expected CEFR source oxford, got %q", results[0].CEFRSource)
	}
}

func TestSearchWords_MapsCEFRLevelAndSource(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		searchWordsFunc: func(_ context.Context, keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]repository.Word, int64, error) {
			return []repository.Word{{
				ID:         2,
				Headword:   "test",
				CEFRLevel:  4,
				CEFRSource: "both",
				Senses: []repository.Sense{{
					POS: 1,
				}},
			}}, 1, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	results, _, err := service.SearchWords(context.Background(), "test", nil, nil, nil, nil, nil, nil, 20, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	if results[0].CEFRLevel != "B2" {
		t.Fatalf("Expected CEFR level B2, got %q", results[0].CEFRLevel)
	}

	if results[0].CEFRSource != "both" {
		t.Fatalf("Expected CEFR source both, got %q", results[0].CEFRSource)
	}
}

func TestSuggestWords_MapsCEFRLevel(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		suggestWordsFunc: func(_ context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]repository.Word, error) {
			return []repository.Word{{
				Headword:   "learn",
				CEFRLevel:  5,
				CEFRSource: "cefrj",
			}}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	results, err := service.SuggestWords(context.Background(), "lea", nil, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	if results[0].CEFRLevel != "C1" {
		t.Fatalf("Expected CEFR level C1, got %q", results[0].CEFRLevel)
	}
}

func TestSearchPhrases_MapsCEFRLevel(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		searchPhrasesFunc: func(_ context.Context, keyword string, limit int) ([]repository.Word, error) {
			return []repository.Word{{
				Headword:   "look up",
				CEFRLevel:  6,
				CEFRSource: "oxford",
			}}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	results, err := service.SearchPhrases(context.Background(), "look", 10)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	if results[0].CEFRLevel != "C2" {
		t.Fatalf("Expected CEFR level C2, got %q", results[0].CEFRLevel)
	}
}

func TestGetWordByHeadword_MapsWordAndSenseCEFRLevels(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordByHeadwordFunc: func(_ context.Context, headword string, includeVariants, includePronunciations, includeSenses bool) (*repository.Word, *repository.WordVariant, error) {
			return &repository.Word{
				ID:         3,
				Headword:   "learn",
				CEFRLevel:  2,
				CEFRSource: "both",
				Senses: []repository.Sense{{
					ID:           7,
					POS:          1,
					CEFRLevel:    3,
					CEFRSource:   "cefrj",
					DefinitionEN: "to gain knowledge",
					DefinitionZH: "学习",
					SenseOrder:   1,
				}},
			}, nil, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	result, err := service.GetWordByHeadword(context.Background(), "learn", nil, false, false, true)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.CEFRLevel != "A2" {
		t.Fatalf("Expected word CEFR level A2, got %q", result.CEFRLevel)
	}

	if len(result.Senses) != 1 {
		t.Fatalf("Expected 1 sense, got %d", len(result.Senses))
	}

	if result.Senses[0].CEFRLevel != "B1" {
		t.Fatalf("Expected sense CEFR level B1, got %q", result.Senses[0].CEFRLevel)
	}

	if result.Senses[0].CEFRSource != "cefrj" {
		t.Fatalf("Expected sense CEFR source cefrj, got %q", result.Senses[0].CEFRSource)
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
	getWordsByVariantsCalls := 0

	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(_ context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			getWordsByHeadwordsCalled = true
			// Only return "apple", "book" is not found in main table
			return []repository.Word{
				{ID: 1, Headword: "apple"},
			}, nil
		},
		getWordsByVariantsFunc: func(_ context.Context, variants []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.BatchVariantMatch, error) {
			getWordsByVariantsCalls++
			expected := []string{"book", "xyz"}
			if !reflect.DeepEqual(variants, expected) {
				t.Fatalf("expected batch variant query %v, got %v", expected, variants)
			}
			return []repository.BatchVariantMatch{{
				Word:    repository.Word{ID: 2, Headword: "book"},
				Variant: repository.WordVariant{WordID: 2, VariantText: "book"},
			}}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	req := &model.BatchRequest{
		Words: []string{"apple", "book", "xyz"},
	}

	responses, meta, err := service.GetWordsBatch(context.Background(), req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify batch query was called
	if !getWordsByHeadwordsCalled {
		t.Error("Expected GetWordsByHeadwords to be called")
	}

	// Verify a single batch variant query was triggered for unresolved words.
	if getWordsByVariantsCalls != 1 {
		t.Errorf("Expected 1 batch fallback query, got %d", getWordsByVariantsCalls)
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
		getWordsByHeadwordsFunc: func(_ context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
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

	responses, meta, err := service.GetWordsBatch(context.Background(), req)
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
		getWordsByHeadwordsFunc: func(_ context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
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

	responses, meta, err := service.GetWordsBatch(context.Background(), req)
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
			name:        "keyword too short (2 chars)",
			keyword:     "ab",
			expectError: true,
			errorMsg:    "at least 3 characters",
		},
		{
			name:        "keyword too short after normalization",
			keyword:     "a-b",
			expectError: true,
			errorMsg:    "at least 3 characters",
		},
		{
			name:        "keyword too long (>100 chars)",
			keyword:     string(make([]rune, 101)),
			expectError: true,
			errorMsg:    "not exceed 100 characters",
		},
		{
			name:        "minimum valid length (3 chars)",
			keyword:     "abc",
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
			_, _, err := service.SearchWords(context.Background(), tt.keyword, nil, nil, nil, nil, nil, nil, 10, 0)
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
			name:        "prefix too short (2 chars)",
			prefix:      "ab",
			expectError: true,
			errorMsg:    "at least 3 characters",
		},
		{
			name:        "prefix too short after normalization",
			prefix:      "a_a",
			expectError: true,
			errorMsg:    "at least 3 characters",
		},
		{
			name:        "prefix too long (>50 chars)",
			prefix:      string(make([]rune, 51)),
			expectError: true,
			errorMsg:    "not exceed 50 characters",
		},
		{
			name:        "minimum valid length (3 chars)",
			prefix:      "abc",
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
			_, err := service.SuggestWords(context.Background(), tt.prefix, nil, nil, nil, nil, nil, 10)
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
		getWordsByHeadwordsFunc: func(_ context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			// Main table returns: "apple", "air conditioning"
			return []repository.Word{
				{ID: 1, Headword: "apple"},
				{ID: 2, Headword: "air conditioning"},
			}, nil
		},
		getWordsByVariantsFunc: func(_ context.Context, variants []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.BatchVariantMatch, error) {
			expected := []string{"lit", "nonexistent"}
			if !reflect.DeepEqual(variants, expected) {
				t.Fatalf("expected batch variant query %v, got %v", expected, variants)
			}
			return []repository.BatchVariantMatch{{
				Word:    repository.Word{ID: 3, Headword: "light"},
				Variant: repository.WordVariant{WordID: 3, VariantText: "lit"},
			}}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	req := &model.BatchRequest{
		Words: []string{"apple", "air-conditioning", "lit", "nonexistent"},
	}

	responses, meta, err := service.GetWordsBatch(context.Background(), req)
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

func TestGetWordsBatch_BatchVariantFallbackPreservesOrderAndNotFound(t *testing.T) {
	cfg := createTestConfig()

	batchFallbackCalls := 0
	individualFallbackCalls := 0

	mockRepo := &mockRepository{
		getWordByHeadwordFunc: func(_ context.Context, headword string, includeVariants, includePronunciations, includeSenses bool) (*repository.Word, *repository.WordVariant, error) {
			individualFallbackCalls++
			t.Fatalf("unexpected per-word fallback for %q", headword)
			return nil, nil, errors.New("unexpected per-word fallback")
		},
		getWordsByHeadwordsFunc: func(_ context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			return []repository.Word{{ID: 1, Headword: "apple"}}, nil
		},
		getWordsByVariantsFunc: func(_ context.Context, variants []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.BatchVariantMatch, error) {
			batchFallbackCalls++
			expected := []string{"learnt", "Lit", "missing"}
			if !reflect.DeepEqual(variants, expected) {
				t.Fatalf("expected batch fallback variants %v, got %v", expected, variants)
			}

			return []repository.BatchVariantMatch{
				{
					Word:    repository.Word{ID: 3, Headword: "Light", FrequencyCount: 120},
					Variant: repository.WordVariant{WordID: 3, VariantText: "Lit", FrequencyRank: 10, FrequencyCount: 30},
				},
				{
					Word:    repository.Word{ID: 2, Headword: "learn", FrequencyCount: 200},
					Variant: repository.WordVariant{WordID: 2, VariantText: "learnt", FrequencyRank: 0, FrequencyCount: 50},
				},
			}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	responses, meta, err := service.GetWordsBatch(context.Background(), &model.BatchRequest{
		Words: []string{"apple", "learnt", "Lit", "missing"},
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if batchFallbackCalls != 1 {
		t.Fatalf("expected 1 batch fallback call, got %d", batchFallbackCalls)
	}
	if individualFallbackCalls != 0 {
		t.Fatalf("expected no per-word fallback calls, got %d", individualFallbackCalls)
	}
	assertBatchVariantFallbackResponses(t, responses)
	assertBatchVariantFallbackMeta(t, meta)
}

func assertBatchVariantFallbackResponses(t *testing.T, responses []model.WordResponse) {
	t.Helper()
	if len(responses) != 3 {
		t.Fatalf("Expected 3 responses, got %d", len(responses))
	}
	expectedHeadwords := []string{"apple", "learn", "Light"}
	for i, expected := range expectedHeadwords {
		if responses[i].Headword != expected {
			t.Fatalf("response[%d] headword = %q, want %q", i, responses[i].Headword, expected)
		}
	}
	if responses[0].QueriedVariant != nil {
		t.Fatalf("expected direct match to have no queried_variant, got %#v", responses[0].QueriedVariant)
	}
	if responses[1].QueriedVariant == nil || responses[1].QueriedVariant.Text != "learnt" {
		t.Fatalf("expected learnt queried_variant metadata, got %#v", responses[1].QueriedVariant)
	}
	if responses[1].QueriedVariant.FrequencyRank != 0 {
		t.Fatalf("expected learnt queried_variant frequency_rank=0, got %#v", responses[1].QueriedVariant)
	}
	if responses[2].QueriedVariant == nil || responses[2].QueriedVariant.Text != "Lit" {
		t.Fatalf("expected Lit queried_variant metadata, got %#v", responses[2].QueriedVariant)
	}
}

func assertBatchVariantFallbackMeta(t *testing.T, meta *model.MetaInfo) {
	t.Helper()
	if meta == nil {
		t.Fatal("Expected meta info, got nil")
	}
	if *meta.Requested != 4 {
		t.Fatalf("expected requested=4, got %d", *meta.Requested)
	}
	if *meta.Found != 3 {
		t.Fatalf("expected found=3, got %d", *meta.Found)
	}
	if !reflect.DeepEqual(meta.NotFound, []string{"missing"}) {
		t.Fatalf("expected not_found [missing], got %v", meta.NotFound)
	}
}

func TestGetWordsBatch_BatchVariantFallbackPreservesCaseSelection(t *testing.T) {
	cfg := createTestConfig()

	mockRepo := &mockRepository{
		getWordsByHeadwordsFunc: func(_ context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.Word, error) {
			return []repository.Word{}, nil
		},
		getWordsByVariantsFunc: func(_ context.Context, variants []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.BatchVariantMatch, error) {
			expected := []string{"Polish", "POLISH"}
			if !reflect.DeepEqual(variants, expected) {
				t.Fatalf("expected batch fallback variants %v, got %v", expected, variants)
			}

			return []repository.BatchVariantMatch{
				{
					Word:    repository.Word{ID: 10, Headword: "Polish"},
					Variant: repository.WordVariant{WordID: 10, VariantText: "Polish"},
				},
				{
					Word:    repository.Word{ID: 11, Headword: "polish"},
					Variant: repository.WordVariant{WordID: 11, VariantText: "POLISH"},
				},
			}, nil
		},
	}

	service := NewWordService(mockRepo, cfg)

	responses, meta, err := service.GetWordsBatch(context.Background(), &model.BatchRequest{
		Words: []string{"Polish", "POLISH"},
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(responses) != 2 {
		t.Fatalf("Expected 2 responses, got %d", len(responses))
	}
	if responses[0].Headword != "Polish" {
		t.Fatalf("expected exact-case match 'Polish', got %q", responses[0].Headword)
	}
	if responses[1].Headword != "polish" {
		t.Fatalf("expected lowercase fallback 'polish', got %q", responses[1].Headword)
	}

	if meta == nil {
		t.Fatal("Expected meta info, got nil")
	}
	if *meta.Found != 2 {
		t.Fatalf("expected found=2, got %d", *meta.Found)
	}
	if len(meta.NotFound) != 0 {
		t.Fatalf("expected no not_found entries, got %v", meta.NotFound)
	}
}
