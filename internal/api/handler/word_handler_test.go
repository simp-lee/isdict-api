package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/simp-lee/isdict-api/internal/api/middleware"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/model"
	"github.com/simp-lee/isdict-data/repository"
	"github.com/simp-lee/isdict-data/service"
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

func TestParseAccent_TrimmedInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?accent=%20british%20", nil)

	result, ok := parseAccent(c, "accent")
	if !ok {
		t.Fatal("parseAccent returned not ok")
	}
	if result == nil {
		t.Fatal("Expected parsed accent, got nil")
	}
	if *result != 1 {
		t.Fatalf("Expected %d, got %d", 1, *result)
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
			name:     "A1 uppercase",
			param:    "A1",
			expected: func() *int { v := 1; return &v }(),
		},
		{
			name:     "A2 uppercase",
			param:    "A2",
			expected: func() *int { v := 2; return &v }(),
		},
		{
			name:     "B1 uppercase",
			param:    "B1",
			expected: func() *int { v := 3; return &v }(),
		},
		{
			name:     "B2 uppercase",
			param:    "B2",
			expected: func() *int { v := 4; return &v }(),
		},
		{
			name:     "C1 uppercase",
			param:    "C1",
			expected: func() *int { v := 5; return &v }(),
		},
		{
			name:     "C2 uppercase",
			param:    "C2",
			expected: func() *int { v := 6; return &v }(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?cefr_level="+url.QueryEscape(tt.param), nil)

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

func TestParsePOS_TrimmedInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?pos=%20noun%20", nil)

	result, ok := parsePOS(c, "pos")
	if !ok {
		t.Fatal("parsePOS returned not ok")
	}
	if result == nil {
		t.Fatal("Expected parsed pos, got nil")
	}
	if *result != 1 {
		t.Fatalf("Expected %d, got %d", 1, *result)
	}
}

func TestParseCEFRLevel_Empty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	result, ok := parseCEFRLevel(c, "cefr_level")
	if !ok {
		t.Fatal("parseCEFRLevel returned not ok for empty value")
	}
	if result != nil {
		t.Fatalf("Expected nil for empty value, got %v", result)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("Expected no response body for empty value, got %q", w.Body.String())
	}
}

func TestParseCEFRLevel_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name     string
		param    string
		expected int
	}{
		{name: "lowercase a2", param: "a2", expected: 2},
		{name: "mixed case b1", param: "b1", expected: 3},
		{name: "mixed case c1", param: "c1", expected: 5},
		{name: "lowercase c2", param: "c2", expected: 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?cefr_level="+url.QueryEscape(tt.param), nil)

			result, ok := parseCEFRLevel(c, "cefr_level")
			if !ok {
				t.Fatal("parseCEFRLevel returned not ok")
			}
			if result == nil {
				t.Fatal("Expected parsed CEFR level, got nil")
			}
			if *result != tt.expected {
				t.Fatalf("Expected %d, got %d", tt.expected, *result)
			}
		})
	}
}

func TestParseCEFRLevel_TrimmedInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?cefr_level=%20b2%20", nil)

	result, ok := parseCEFRLevel(c, "cefr_level")
	if !ok {
		t.Fatal("parseCEFRLevel returned not ok")
	}
	if result == nil {
		t.Fatal("Expected parsed CEFR level, got nil")
	}
	if *result != 4 {
		t.Fatalf("Expected %d, got %d", 4, *result)
	}
}

func TestParseCEFRLevel_Invalid(t *testing.T) {
	tests := []struct {
		name            string
		param           string
		expectedMessage string
	}{
		{"integer code", "3", "cefr_level must be one of: A1, A2, B1, B2, C1, C2"},
		{"negative integer", "-1", "cefr_level must be one of: A1, A2, B1, B2, C1, C2"},
		{"text value", "abc", "cefr_level must be one of: A1, A2, B1, B2, C1, C2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?cefr_level="+url.QueryEscape(tt.param), nil)

			_, ok := parseCEFRLevel(c, "cefr_level")
			if ok {
				t.Error("Expected parseCEFRLevel to fail")
			}

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status 400, got %d", w.Code)
			}

			var resp struct {
				Success bool             `json:"success"`
				Error   *model.ErrorInfo `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}
			if resp.Error == nil {
				t.Fatal("Expected error response body")
			}
			if resp.Error.Code != "INVALID_PARAMETER" {
				t.Fatalf("Expected INVALID_PARAMETER error, got %q", resp.Error.Code)
			}
			if resp.Error.Message != tt.expectedMessage {
				t.Fatalf("Expected error message %q, got %q", tt.expectedMessage, resp.Error.Message)
			}
		})
	}
}

func TestParseOxfordLevel(t *testing.T) {
	tests := []struct {
		name        string
		param       string
		expected    *int
		wantOK      bool
		wantStatus  int
		wantMessage string
	}{
		{
			name:     "empty means any",
			param:    "",
			expected: nil,
			wantOK:   true,
		},
		{
			name:     "zero means any",
			param:    "0",
			expected: nil,
			wantOK:   true,
		},
		{
			name:     "oxford 3000",
			param:    "1",
			expected: func() *int { v := 1; return &v }(),
			wantOK:   true,
		},
		{
			name:     "oxford 5000",
			param:    "2",
			expected: func() *int { v := 2; return &v }(),
			wantOK:   true,
		},
		{
			name:        "invalid value",
			param:       "3",
			wantOK:      false,
			wantStatus:  http.StatusBadRequest,
			wantMessage: "oxford_level must be 0 (any), 1 (Oxford 3000), or 2 (Oxford 5000)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?oxford_level="+url.QueryEscape(tt.param), nil)

			result, ok := parseOxfordLevel(c, "oxford_level")
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}

			if tt.wantOK {
				if tt.expected == nil {
					if result != nil {
						t.Fatalf("Expected nil, got %v", result)
					}
				} else {
					if result == nil {
						t.Fatalf("Expected %d, got nil", *tt.expected)
					}
					if *result != *tt.expected {
						t.Fatalf("Expected %d, got %d", *tt.expected, *result)
					}
				}
				return
			}

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			var resp struct {
				Success bool             `json:"success"`
				Error   *model.ErrorInfo `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}
			if resp.Error == nil {
				t.Fatal("Expected error response body")
			}
			if resp.Error.Message != tt.wantMessage {
				t.Fatalf("Expected error message %q, got %q", tt.wantMessage, resp.Error.Message)
			}
		})
	}
}

func TestParseNumericParameters_TrimmedInput(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		parse     func(*gin.Context) (int, bool, bool)
		wantValue int
	}{
		{
			name: "oxford level",
			path: "/?oxford_level=%201%20",
			parse: func(c *gin.Context) (int, bool, bool) {
				result, ok := parseOxfordLevel(c, "oxford_level")
				if result == nil {
					return 0, false, ok
				}
				return *result, true, ok
			},
			wantValue: 1,
		},
		{
			name: "cet level",
			path: "/?cet_level=%206%20",
			parse: func(c *gin.Context) (int, bool, bool) {
				result, ok := parseCETLevel(c, "cet_level")
				if result == nil {
					return 0, false, ok
				}
				return *result, true, ok
			},
			wantValue: 2,
		},
		{
			name: "max frequency rank",
			path: "/?max_frequency_rank=%20100%20",
			parse: func(c *gin.Context) (int, bool, bool) {
				result, ok := parseMaxFrequencyRank(c, "max_frequency_rank")
				if result == nil {
					return 0, false, ok
				}
				return *result, true, ok
			},
			wantValue: 100,
		},
		{
			name: "min collins stars",
			path: "/?min_collins_stars=%203%20",
			parse: func(c *gin.Context) (int, bool, bool) {
				result, ok := parseMinCollinsStars(c, "min_collins_stars")
				if result == nil {
					return 0, false, ok
				}
				return *result, true, ok
			},
			wantValue: 3,
		},
		{
			name: "limit",
			path: "/?limit=%205%20",
			parse: func(c *gin.Context) (int, bool, bool) {
				result, ok := parseLimit(c, 20, 100)
				return result, true, ok
			},
			wantValue: 5,
		},
		{
			name: "offset",
			path: "/?offset=%207%20",
			parse: func(c *gin.Context) (int, bool, bool) {
				result, ok := parseOffset(c)
				return result, true, ok
			},
			wantValue: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", tt.path, nil)

			got, present, ok := tt.parse(c)
			if !ok {
				t.Fatal("expected parser to accept trimmed numeric input")
			}
			if !present {
				t.Fatal("expected parsed numeric value, got nil")
			}
			if got != tt.wantValue {
				t.Fatalf("got %d, want %d", got, tt.wantValue)
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
		{"trimmed true", " true ", true},
		{"trimmed false", " false ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?param="+url.QueryEscape(tt.param), nil)

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

func TestValidateHeadword_TrimmedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "headword", Value: "  apple  "}}

	headword, ok := validateHeadword(c)
	if !ok {
		t.Fatal("Expected validateHeadword to accept padded headword")
	}

	if headword != "apple" {
		t.Fatalf("Expected trimmed headword %q, got %q", "apple", headword)
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
	service := service.NewWordService(repo, service.ServiceConfig{})
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

func TestSearchWords_InternalErrorIsSanitizedAndLogged(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &stubVariantRepository{searchErr: errors.New("database connection reset")}
	cfg := &config.Config{APISearchMaxLimit: 50}
	service := service.NewWordService(repo, service.ServiceConfig{SearchMaxLimit: 50})
	handler := NewWordHandler(service, cfg)

	var logBuffer bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuffer, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	router := gin.New()
	router.Use(middleware.SetupMiddleware(&config.Config{EnableRequestID: true}))
	router.GET("/api/v1/search", handler.SearchWords)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=apple", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status 500, got %d", w.Code)
	}

	var resp struct {
		Success bool             `json:"success"`
		Error   *model.ErrorInfo `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("Expected error response body")
	}
	if resp.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("Expected INTERNAL_ERROR, got %q", resp.Error.Code)
	}
	if resp.Error.Message != "An internal error occurred" {
		t.Fatalf("Expected sanitized error message, got %q", resp.Error.Message)
	}
	if strings.Contains(w.Body.String(), "database connection reset") {
		t.Fatalf("Expected response body to be sanitized, got %q", w.Body.String())
	}

	requestID := w.Header().Get("X-Request-Id")
	if requestID == "" {
		t.Fatal("Expected request ID header to be present")
	}

	logOutput := logBuffer.String()
	if !strings.Contains(logOutput, `"handler":"SearchWords"`) {
		t.Fatalf("Expected handler name in log output, got %q", logOutput)
	}
	if !strings.Contains(logOutput, `"request_id":"`+requestID+`"`) {
		t.Fatalf("Expected request ID in log output, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "database connection reset") {
		t.Fatalf("Expected underlying error in log output, got %q", logOutput)
	}
}

func TestGetWordsBatch_InternalErrorIsSanitizedAndLogged(t *testing.T) {
	gin.SetMode(gin.TestMode)

	serviceErr := errors.New("decorated batch failure")
	handler := NewWordHandler(stubWordService{
		getWordsBatchFunc: func(ctx context.Context, req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error) {
			return nil, nil, serviceErr
		},
	}, &config.Config{APIBatchMaxSize: 10})

	var logBuffer bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuffer, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	router := gin.New()
	router.Use(middleware.SetupMiddleware(&config.Config{EnableRequestID: true}))
	router.POST("/api/v1/words/batch", handler.GetWordsBatch)

	body := bytes.NewBufferString(`{"words":["apple","pear"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/words/batch", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status 500, got %d", w.Code)
	}

	var resp struct {
		Success bool             `json:"success"`
		Error   *model.ErrorInfo `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("Expected error response body")
	}
	if resp.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("Expected INTERNAL_ERROR, got %q", resp.Error.Code)
	}
	if resp.Error.Message != "An internal error occurred" {
		t.Fatalf("Expected sanitized error message, got %q", resp.Error.Message)
	}
	if strings.Contains(w.Body.String(), serviceErr.Error()) {
		t.Fatalf("Expected response body to be sanitized, got %q", w.Body.String())
	}

	requestID := w.Header().Get("X-Request-Id")
	if requestID == "" {
		t.Fatal("Expected request ID header to be present")
	}

	logOutput := logBuffer.String()
	if !strings.Contains(logOutput, `"handler":"GetWordsBatch"`) {
		t.Fatalf("Expected handler name in log output, got %q", logOutput)
	}
	if !strings.Contains(logOutput, `"request_id":"`+requestID+`"`) {
		t.Fatalf("Expected request ID in log output, got %q", logOutput)
	}
	if !strings.Contains(logOutput, serviceErr.Error()) {
		t.Fatalf("Expected underlying error in log output, got %q", logOutput)
	}
}

func TestGetWordsBatch_AllowsRawOversizedInputWhenCleanedBatchFitsLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	serviceCalled := false
	handler := NewWordHandler(stubWordService{
		getWordsBatchFunc: func(ctx context.Context, req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error) {
			serviceCalled = true
			expected := []string{"apple", "pear"}
			if !reflect.DeepEqual(req.Words, expected) {
				t.Fatalf("expected cleaned words %v, got %v", expected, req.Words)
			}
			requested := len(req.Words)
			found := requested
			return []model.WordResponse{
				{ID: 1, Headword: "apple"},
				{ID: 2, Headword: "pear"},
			}, &model.MetaInfo{Requested: &requested, Found: &found}, nil
		},
	}, &config.Config{APIBatchMaxSize: 2})

	router := gin.New()
	router.Use(middleware.SetupMiddleware(&config.Config{EnableRequestID: true}))
	router.POST("/api/v1/words/batch", handler.GetWordsBatch)

	body := bytes.NewBufferString(`{"words":[" apple ","","apple","pear","  ","pear"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/words/batch", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d; body=%s", w.Code, w.Body.String())
	}
	if !serviceCalled {
		t.Fatal("Expected service to be called")
	}

	var resp struct {
		Success bool                 `json:"success"`
		Data    []model.WordResponse `json:"data"`
		Meta    *model.MetaInfo      `json:"meta"`
		Error   *model.ErrorInfo     `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("Expected success response, got %+v", resp)
	}
	if resp.Error != nil {
		t.Fatalf("Expected no error payload, got %+v", resp.Error)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("Expected 2 words in response, got %d", len(resp.Data))
	}
	if resp.Meta == nil || resp.Meta.Requested == nil || *resp.Meta.Requested != 2 {
		t.Fatalf("Expected requested=2 after cleanup, got %+v", resp.Meta)
	}
}

func TestHandlers_PropagateRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type contextKey string
	const requestContextKey contextKey = "request-context-key"
	const requestContextValue = "request-context-value"

	cfg := &config.Config{
		APIBatchMaxSize:    10,
		APISearchMaxLimit:  50,
		APISuggestMaxLimit: 20,
	}

	tests := []struct {
		name          string
		method        string
		target        string
		body          string
		contentType   string
		registerRoute func(*gin.Engine, *WordHandler)
		service       func(*context.Context) stubWordService
	}{
		{
			name:   "GetWord",
			method: http.MethodGet,
			target: "/api/v1/words/apple",
			registerRoute: func(router *gin.Engine, handler *WordHandler) {
				router.GET("/api/v1/words/:headword", handler.GetWord)
			},
			service: func(receivedCtx *context.Context) stubWordService {
				return stubWordService{
					getWordByHeadwordFunc: func(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error) {
						*receivedCtx = ctx
						return &model.WordResponse{}, nil
					},
				}
			},
		},
		{
			name:   "GetWordByVariant",
			method: http.MethodGet,
			target: "/api/v1/words/by-variant/apple",
			registerRoute: func(router *gin.Engine, handler *WordHandler) {
				router.GET("/api/v1/words/by-variant/:variant", handler.GetWordByVariant)
			},
			service: func(receivedCtx *context.Context) stubWordService {
				return stubWordService{
					getWordsByVariantFunc: func(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error) {
						*receivedCtx = ctx
						return []model.VariantReverseResponse{}, nil
					},
				}
			},
		},
		{
			name:        "GetWordsBatch",
			method:      http.MethodPost,
			target:      "/api/v1/words/batch",
			body:        `{"words":["apple"]}`,
			contentType: "application/json",
			registerRoute: func(router *gin.Engine, handler *WordHandler) {
				router.POST("/api/v1/words/batch", handler.GetWordsBatch)
			},
			service: func(receivedCtx *context.Context) stubWordService {
				return stubWordService{
					getWordsBatchFunc: func(ctx context.Context, req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error) {
						*receivedCtx = ctx
						return []model.WordResponse{}, &model.MetaInfo{}, nil
					},
				}
			},
		},
		{
			name:   "SearchWords",
			method: http.MethodGet,
			target: "/api/v1/search?q=apple",
			registerRoute: func(router *gin.Engine, handler *WordHandler) {
				router.GET("/api/v1/search", handler.SearchWords)
			},
			service: func(receivedCtx *context.Context) stubWordService {
				return stubWordService{
					searchWordsFunc: func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
						*receivedCtx = ctx
						return []model.SearchResultResponse{}, &model.MetaInfo{}, nil
					},
				}
			},
		},
		{
			name:   "SuggestWords",
			method: http.MethodGet,
			target: "/api/v1/suggest?prefix=apple",
			registerRoute: func(router *gin.Engine, handler *WordHandler) {
				router.GET("/api/v1/suggest", handler.SuggestWords)
			},
			service: func(receivedCtx *context.Context) stubWordService {
				return stubWordService{
					suggestWordsFunc: func(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
						*receivedCtx = ctx
						return []model.SuggestResponse{}, nil
					},
				}
			},
		},
		{
			name:   "SearchPhrases",
			method: http.MethodGet,
			target: "/api/v1/phrases?q=apple",
			registerRoute: func(router *gin.Engine, handler *WordHandler) {
				router.GET("/api/v1/phrases", handler.SearchPhrases)
			},
			service: func(receivedCtx *context.Context) stubWordService {
				return stubWordService{
					searchPhrasesFunc: func(ctx context.Context, keyword string, limit int) ([]model.SuggestResponse, error) {
						*receivedCtx = ctx
						return []model.SuggestResponse{}, nil
					},
				}
			},
		},
		{
			name:   "GetPronunciations",
			method: http.MethodGet,
			target: "/api/v1/words/apple/pronunciations",
			registerRoute: func(router *gin.Engine, handler *WordHandler) {
				router.GET("/api/v1/words/:headword/pronunciations", handler.GetPronunciations)
			},
			service: func(receivedCtx *context.Context) stubWordService {
				return stubWordService{
					getPronunciationsFunc: func(ctx context.Context, headword string, accentCode *int) ([]model.PronunciationResponse, error) {
						*receivedCtx = ctx
						return []model.PronunciationResponse{}, nil
					},
				}
			},
		},
		{
			name:   "GetSenses",
			method: http.MethodGet,
			target: "/api/v1/words/apple/senses",
			registerRoute: func(router *gin.Engine, handler *WordHandler) {
				router.GET("/api/v1/words/:headword/senses", handler.GetSenses)
			},
			service: func(receivedCtx *context.Context) stubWordService {
				return stubWordService{
					getSensesFunc: func(ctx context.Context, headword string, posCode *int, lang string) ([]model.SenseResponse, error) {
						*receivedCtx = ctx
						return []model.SenseResponse{}, nil
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedCtx context.Context

			handler := NewWordHandler(tt.service(&receivedCtx), cfg)
			router := gin.New()
			tt.registerRoute(router, handler)

			var body *bytes.Buffer
			if tt.body == "" {
				body = bytes.NewBuffer(nil)
			} else {
				body = bytes.NewBufferString(tt.body)
			}

			ctxWithValue := context.WithValue(context.Background(), requestContextKey, requestContextValue)
			req := httptest.NewRequest(tt.method, tt.target, body).WithContext(ctxWithValue)
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected status 200, got %d; body=%s", w.Code, w.Body.String())
			}
			if receivedCtx == nil {
				t.Fatal("Expected context to be captured")
			}
			if got := receivedCtx.Value(requestContextKey); got != requestContextValue {
				t.Fatalf("Expected propagated context value %q, got %v", requestContextValue, got)
			}
		})
	}
}

type stubVariantRepository struct {
	words        []model.Word
	variants     []model.WordVariant
	expectedKind int
	enforceKind  bool
	searchErr    error
	searchCtx    context.Context
}

func (s *stubVariantRepository) GetWordByHeadword(ctx context.Context, headword string, includeVariants, includePronunciations, includeSenses bool) (*model.Word, *model.WordVariant, error) {
	return nil, nil, repository.ErrWordNotFound
}

func (s *stubVariantRepository) GetWordsByHeadwords(ctx context.Context, headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]model.Word, error) {
	return nil, nil
}

func (s *stubVariantRepository) GetWordsByVariants(ctx context.Context, variants []string, includeVariants, includePronunciations, includeSenses bool) ([]repository.BatchVariantMatch, error) {
	return nil, nil
}

func (s *stubVariantRepository) GetWordsByVariant(ctx context.Context, variant string, kind *int, includePronunciations, includeSenses bool) ([]model.Word, []model.WordVariant, error) {
	if s.enforceKind {
		if kind == nil || *kind != s.expectedKind {
			return nil, nil, repository.ErrVariantNotFound
		}
	}
	return s.words, s.variants, nil
}

func (s *stubVariantRepository) SearchWords(ctx context.Context, keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.Word, int64, error) {
	s.searchCtx = ctx
	if s.searchErr != nil {
		return nil, 0, s.searchErr
	}
	return nil, 0, nil
}

func (s *stubVariantRepository) SuggestWords(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.Word, error) {
	return nil, nil
}

func (s *stubVariantRepository) SearchPhrases(ctx context.Context, keyword string, limit int) ([]model.Word, error) {
	return nil, nil
}

func (s *stubVariantRepository) GetPronunciationsByWordID(ctx context.Context, wordID uint, accent *int) ([]model.Pronunciation, error) {
	return nil, nil
}

func (s *stubVariantRepository) GetSensesByWordID(ctx context.Context, wordID uint, pos *int) ([]model.Sense, error) {
	return nil, nil
}

type stubWordService struct {
	getWordByHeadwordFunc func(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error)
	getWordsByVariantFunc func(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error)
	getWordsBatchFunc     func(ctx context.Context, req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error)
	searchWordsFunc       func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error)
	suggestWordsFunc      func(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error)
	searchPhrasesFunc     func(ctx context.Context, keyword string, limit int) ([]model.SuggestResponse, error)
	getPronunciationsFunc func(ctx context.Context, headword string, accentCode *int) ([]model.PronunciationResponse, error)
	getSensesFunc         func(ctx context.Context, headword string, posCode *int, lang string) ([]model.SenseResponse, error)
}

func (s stubWordService) GetWordByHeadword(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error) {
	if s.getWordByHeadwordFunc == nil {
		return nil, fmt.Errorf("unexpected GetWordByHeadword call")
	}
	return s.getWordByHeadwordFunc(ctx, headword, accentCode, includeVariants, includePronunciations, includeSenses)
}

func (s stubWordService) GetWordsByVariant(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error) {
	if s.getWordsByVariantFunc == nil {
		return nil, fmt.Errorf("unexpected GetWordsByVariant call")
	}
	return s.getWordsByVariantFunc(ctx, variant, kindStr, includePronunciations, includeSenses)
}

func (s stubWordService) GetWordsBatch(ctx context.Context, req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error) {
	if s.getWordsBatchFunc == nil {
		return nil, nil, fmt.Errorf("unexpected GetWordsBatch call")
	}
	return s.getWordsBatchFunc(ctx, req)
}

func (s stubWordService) SearchWords(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
	if s.searchWordsFunc == nil {
		return nil, nil, fmt.Errorf("unexpected SearchWords call")
	}
	return s.searchWordsFunc(ctx, keyword, posCode, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit, offset)
}

func (s stubWordService) SuggestWords(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
	if s.suggestWordsFunc == nil {
		return nil, fmt.Errorf("unexpected SuggestWords call")
	}
	return s.suggestWordsFunc(ctx, prefix, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit)
}

func (s stubWordService) SearchPhrases(ctx context.Context, keyword string, limit int) ([]model.SuggestResponse, error) {
	if s.searchPhrasesFunc == nil {
		return nil, fmt.Errorf("unexpected SearchPhrases call")
	}
	return s.searchPhrasesFunc(ctx, keyword, limit)
}

func (s stubWordService) GetPronunciations(ctx context.Context, headword string, accentCode *int) ([]model.PronunciationResponse, error) {
	if s.getPronunciationsFunc == nil {
		return nil, fmt.Errorf("unexpected GetPronunciations call")
	}
	return s.getPronunciationsFunc(ctx, headword, accentCode)
}

func (s stubWordService) GetSenses(ctx context.Context, headword string, posCode *int, lang string) ([]model.SenseResponse, error) {
	if s.getSensesFunc == nil {
		return nil, fmt.Errorf("unexpected GetSenses call")
	}
	return s.getSensesFunc(ctx, headword, posCode, lang)
}

func intPtr(v int) *int {
	return &v
}
