package repository

import (
	"testing"
)

func TestEscapeLikePattern(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no special characters",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "percent sign",
			input:    "50%",
			expected: "50\\%",
		},
		{
			name:     "underscore",
			input:    "C++_test",
			expected: "C++\\_test",
		},
		{
			name:     "backslash",
			input:    "path\\to\\file",
			expected: "path\\\\to\\\\file",
		},
		{
			name:     "multiple wildcards",
			input:    "%_test_%",
			expected: "\\%\\_test\\_\\%",
		},
		{
			name:     "backslash before percent",
			input:    "\\%test",
			expected: "\\\\\\%test",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "real example: C++",
			input:    "C++",
			expected: "C++",
		},
		{
			name:     "real example: 100%",
			input:    "100%",
			expected: "100\\%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeLikePattern(tt.input)
			if result != tt.expected {
				t.Errorf("escapeLikePattern(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

/*
NOTE: The following tests require a live database connection and are meant to be
run as integration tests. To enable them:

1. Set up a test database with sample data
2. Set environment variable: TEST_DB_ENABLED=true
3. Configure connection in test setup

Example setup:
	export TEST_DB_ENABLED=true
	export TEST_DB_DSN="host=localhost port=5432 user=test password=test dbname=lexiforge_test"

These tests verify:
- GetWordByHeadword: main word hit vs variant fallback
- Variant fallback: correct selection when multiple words match
- SearchWords: no duplicates when variants trigger matches
*/

// TestGetWordByHeadword_MainWordHit would test direct match in words table
// func TestGetWordByHeadword_MainWordHit(t *testing.T) {
// 	if !isIntegrationTestEnabled() {
// 		t.Skip("Integration tests disabled")
// 	}
//
// 	db := setupTestDB(t)
// 	repo := NewRepository(db)
//
// 	// Test case: query "apple" which exists in main words table
// 	word, err := repo.GetWordByHeadword("apple", true, true, true)
// 	if err != nil {
// 		t.Fatalf("Expected word to be found, got error: %v", err)
// 	}
//
// 	if word.Headword != "apple" {
// 		t.Errorf("Expected headword 'apple', got %s", word.Headword)
// 	}
//
// 	// Verify it was a direct match (not via variant)
// 	// This can be verified by checking query logs or using a spy
// }

// TestGetWordByHeadword_VariantFallback would test fallback to variant table
// func TestGetWordByHeadword_VariantFallback(t *testing.T) {
// 	if !isIntegrationTestEnabled() {
// 		t.Skip("Integration tests disabled")
// 	}
//
// 	db := setupTestDB(t)
// 	repo := NewRepository(db)
//
// 	// Test case: query "air-conditioning" which doesn't exist in main table
// 	// but exists as variant of "air conditioning"
// 	word, err := repo.GetWordByHeadword("air-conditioning", true, true, true)
// 	if err != nil {
// 		t.Fatalf("Expected word to be found via variant, got error: %v", err)
// 	}
//
// 	if word.Headword != "air conditioning" {
// 		t.Errorf("Expected main word 'air conditioning', got %s", word.Headword)
// 	}
// }

// TestGetWordByHeadword_VariantFallback_BestSelection would test variant selection logic
// func TestGetWordByHeadword_VariantFallback_BestSelection(t *testing.T) {
// 	if !isIntegrationTestEnabled() {
// 		t.Skip("Integration tests disabled")
// 	}
//
// 	db := setupTestDB(t)
// 	repo := NewRepository(db)
//
// 	// Setup: Create test data where one variant maps to multiple words
// 	// E.g., "lit" could map to both:
// 	//   - "lit" (adjective, high frequency, CEFR 3)
// 	//   - "light" (noun/verb, very high frequency, CEFR 1)
// 	// Expected: Should return "light" (higher frequency, lower CEFR)
//
// 	word, err := repo.GetWordByHeadword("lit", false, false, false)
// 	if err != nil {
// 	    t.Fatalf("Expected word to be found, got error: %v", err)
// 	}
//
// 	// Verify correct word was selected based on sorting criteria:
// 	// kind ASC, frequency_rank ASC, cefr_level DESC
// 	// For this test, we expect the higher quality word
// 	if word.FrequencyRank == 0 && word.CEFRLevel == 0 {
// 	    t.Error("Expected non-zero quality metrics for selected word")
// 	}
// }

// TestSearchWords_NoDuplicatesWithVariants would test deduplication
// func TestSearchWords_NoDuplicatesWithVariants(t *testing.T) {
// 	if !isIntegrationTestEnabled() {
// 		t.Skip("Integration tests disabled")
// 	}
//
// 	db := setupTestDB(t)
// 	repo := NewRepository(db)
//
// 	// Test case: search "air" which should match:
// 	//   - "air" (main word)
// 	//   - "air conditioning" (main word)
// 	//   - "air conditioning" (via variant "air-conditioning")
// 	// Expected: "air conditioning" should appear only once
//
// 	words, total, err := repo.SearchWords("air", nil, nil, 10, 0)
// 	if err != nil {
// 		t.Fatalf("Search failed: %v", err)
// 	}
//
// 	// Check for duplicates
// 	seen := make(map[uint]bool)
// 	for _, word := range words {
// 		if seen[word.ID] {
// 			t.Errorf("Duplicate word found: ID=%d, Headword=%s", word.ID, word.Headword)
// 		}
// 		seen[word.ID] = true
// 	}
//
// 	if int64(len(words)) > total {
// 		t.Errorf("Returned more words (%d) than total (%d)", len(words), total)
// 	}
// }

// TestGetWordByHeadword_NotFound would test 404 case
// func TestGetWordByHeadword_NotFound(t *testing.T) {
// 	if !isIntegrationTestEnabled() {
// 		t.Skip("Integration tests disabled")
// 	}
//
// 	db := setupTestDB(t)
// 	repo := NewRepository(db)
//
// 	// Query a word that doesn't exist in main table or variants
// 	_, err := repo.GetWordByHeadword("xyznonexistent123", false, false, false)
// 	if err != ErrWordNotFound {
// 		t.Errorf("Expected ErrWordNotFound, got %v", err)
// 	}
// }

// Helper functions for integration tests
// func isIntegrationTestEnabled() bool {
// 	return os.Getenv("TEST_DB_ENABLED") == "true"
// }

// func setupTestDB(t *testing.T) *gorm.DB {
// 	dsn := os.Getenv("TEST_DB_DSN")
// 	if dsn == "" {
// 		t.Fatal("TEST_DB_DSN environment variable not set")
// 	}
//
// 	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
// 	if err != nil {
// 		t.Fatalf("Failed to connect to test database: %v", err)
// 	}
//
// 	return db
// }
