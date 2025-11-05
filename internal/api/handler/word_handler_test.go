package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/simp-lee/isdict-api/internal/api/repository"
	"github.com/simp-lee/isdict-api/internal/api/service"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/model"
)

func TestParseAccent_Valid(t *testing.T) {
	tests := []struct {
		name     string
		param    string
		expected *int
	}{
		{
			name:     "british",
			param:    "british",
			expected: func() *int { v := 1; return &v }(),
		},
		{
			name:     "american",
			param:    "american",
			expected: func() *int { v := 2; return &v }(),
		},
		{
			name:     "empty",
			param:    "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?accent="+tt.param, nil)

			result, ok := parseAccent(c, "accent")
			if !ok {
				t.Fatal("parseAccent returned not ok")
			}

			if tt.expected == nil && result != nil {
				t.Errorf("Expected nil, got %v", result)
			} else if tt.expected != nil && result == nil {
				t.Errorf("Expected %v, got nil", *tt.expected)
			} else if tt.expected != nil && result != nil && *tt.expected != *result {
				t.Errorf("Expected %d, got %d", *tt.expected, *result)
			}
		})
	}
}

func TestParseAccent_Invalid(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?accent=invalid", nil)

	_, ok := parseAccent(c, "accent")
	if ok {
		t.Error("Expected parseAccent to fail for invalid accent")
	}

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestParsePOS_Valid(t *testing.T) {
	tests := []struct {
		name     string
		param    string
		expected *int
	}{
		{
			name:     "noun",
			param:    "noun",
			expected: func() *int { v := 1; return &v }(),
		},
		{
			name:     "verb",
			param:    "verb",
			expected: func() *int { v := 2; return &v }(),
		},
		{
			name:     "empty",
			param:    "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?pos="+tt.param, nil)

			result, ok := parsePOS(c, "pos")
			if !ok {
				t.Fatal("parsePOS returned not ok")
			}

			if tt.expected == nil && result != nil {
				t.Errorf("Expected nil, got %v", result)
			} else if tt.expected != nil && result == nil {
				t.Errorf("Expected %v, got nil", *tt.expected)
			} else if tt.expected != nil && result != nil && *tt.expected != *result {
				t.Errorf("Expected %d, got %d", *tt.expected, *result)
			}
		})
	}
}

func TestParseCEFRLevel_Valid(t *testing.T) {
	tests := []struct {
		name     string
		param    string
		expected *int
	}{
		{
			name:     "level 0",
			param:    "0",
			expected: func() *int { v := 0; return &v }(),
		},
		{
			name:     "level 3",
			param:    "3",
			expected: func() *int { v := 3; return &v }(),
		},
		{
			name:     "level 6",
			param:    "6",
			expected: func() *int { v := 6; return &v }(),
		},
		{
			name:     "empty",
			param:    "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?cefr_level="+tt.param, nil)

			result, ok := parseCEFRLevel(c, "cefr_level")
			if !ok {
				t.Fatal("parseCEFRLevel returned not ok")
			}

			if tt.expected == nil && result != nil {
				t.Errorf("Expected nil, got %v", result)
			} else if tt.expected != nil && result == nil {
				t.Errorf("Expected %v, got nil", *tt.expected)
			} else if tt.expected != nil && result != nil && *tt.expected != *result {
				t.Errorf("Expected %d, got %d", *tt.expected, *result)
			}
		})
	}
}

func TestParseCEFRLevel_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		param string
	}{
		{"negative", "-1"},
		{"too large", "7"},
		{"not a number", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?cefr_level="+tt.param, nil)

			_, ok := parseCEFRLevel(c, "cefr_level")
			if ok {
				t.Error("Expected parseCEFRLevel to fail")
			}

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status 400, got %d", w.Code)
			}
		})
	}
}

func TestParseLimit_Valid(t *testing.T) {
	tests := []struct {
		name         string
		param        string
		defaultLimit int
		maxLimit     int
		expected     int
	}{
		{"valid limit", "10", 20, 100, 10},
		{"empty uses default", "", 20, 100, 20},
		{"max limit uses default", "101", 20, 100, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?limit="+tt.param, nil)

			result, ok := parseLimit(c, tt.defaultLimit, tt.maxLimit)
			if tt.param == "101" {
				// Should fail for exceeding max
				if ok {
					t.Error("Expected parseLimit to fail for limit > max")
				}
			} else {
				if !ok {
					t.Fatal("parseLimit returned not ok")
				}
				if result != tt.expected {
					t.Errorf("Expected %d, got %d", tt.expected, result)
				}
			}
		})
	}
}

func TestParseBool_Valid(t *testing.T) {
	tests := []struct {
		name     string
		param    string
		expected bool
	}{
		{"true", "true", true},
		{"false", "false", false},
		{"True uppercase", "True", true},
		{"FALSE uppercase", "FALSE", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?param="+tt.param, nil)

			result, ok := parseBool(c, "param", false)
			if !ok {
				t.Fatal("parseBool returned not ok")
			}

			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestValidateHeadword_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "headword", Value: "  "}}

	_, ok := validateHeadword(c)
	if ok {
		t.Error("Expected validateHeadword to fail for empty headword")
	}

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestGetWordByVariant_InvalidKindParameter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "variant", Value: "test"}}
	c.Request = httptest.NewRequest("GET", "/api/v1/words/by-variant/test?kind=unknown", nil)

	handler := NewWordHandler(nil, nil)
	handler.GetWordByVariant(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected status 400 for invalid kind, got %d", w.Code)
	}

	var resp struct {
		Success bool             `json:"success"`
		Error   *model.ErrorInfo `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != "INVALID_PARAMETER" {
		t.Fatalf("Expected INVALID_PARAMETER error, got %+v", resp.Error)
	}
}

func TestGetWordByVariant_ValidKindWithWhitespace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "variant", Value: "lit"}}
	c.Request = httptest.NewRequest("GET", "/api/v1/words/by-variant/lit?kind=%20form%20", nil)

	repo := &stubVariantRepository{
		words: []model.Word{
			{
				ID:                 1,
				Headword:           "light",
				HeadwordNormalized: "light",
				CEFRLevel:          1,
				FrequencyRank:      150,
			},
		},
		variants: []model.WordVariant{
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
		},
		expectedKind: int(model.VariantForm),
		enforceKind:  true,
	}

	cfg := &config.Config{}
	service := service.NewWordService(repo, cfg)
	handler := NewWordHandler(service, cfg)

	handler.GetWordByVariant(c)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var resp struct {
		Success bool                           `json:"success"`
		Data    []model.VariantReverseResponse `json:"data"`
		Error   *model.ErrorInfo               `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if !resp.Success {
		t.Fatal("Expected success in response")
	}
	if len(resp.Data) != 1 {
		t.Fatalf("Expected one word result, got %d", len(resp.Data))
	}
	if len(resp.Data[0].VariantInfo) != 2 {
		t.Fatalf("Expected two variant entries, got %d", len(resp.Data[0].VariantInfo))
	}
	if resp.Data[0].VariantInfo[0].FormType != "past" {
		t.Fatalf("Expected first form_type past, got %s", resp.Data[0].VariantInfo[0].FormType)
	}
	if resp.Data[0].VariantInfo[1].FormType != "past_participle" {
		t.Fatalf("Expected second form_type past_participle, got %s", resp.Data[0].VariantInfo[1].FormType)
	}
}

type stubVariantRepository struct {
	words        []model.Word
	variants     []model.WordVariant
	expectedKind int
	enforceKind  bool
}

func (s *stubVariantRepository) GetWordByHeadword(headword string, includeVariants, includePronunciations, includeSenses bool) (*model.Word, *model.WordVariant, error) {
	return nil, nil, repository.ErrWordNotFound
}

func (s *stubVariantRepository) GetWordsByHeadwords(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]model.Word, error) {
	return nil, nil
}

func (s *stubVariantRepository) GetWordsByVariant(variant string, kind *int) ([]model.Word, []model.WordVariant, error) {
	if s.enforceKind {
		if kind == nil || *kind != s.expectedKind {
			return nil, nil, repository.ErrVariantNotFound
		}
	}
	return s.words, s.variants, nil
}

func (s *stubVariantRepository) SearchWords(keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.Word, int64, error) {
	return nil, 0, nil
}

func (s *stubVariantRepository) SuggestWords(prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.Word, error) {
	return nil, nil
}

func (s *stubVariantRepository) SearchPhrases(keyword string, limit int) ([]model.Word, error) {
	return nil, nil
}

func (s *stubVariantRepository) GetPronunciationsByWordID(wordID uint, accent *int) ([]model.Pronunciation, error) {
	return nil, nil
}

func (s *stubVariantRepository) GetSensesByWordID(wordID uint, pos *int) ([]model.Sense, error) {
	return nil, nil
}

func intPtr(v int) *int {
	return &v
}
