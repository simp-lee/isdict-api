package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/isdict-api/internal/api/contracts"
	"github.com/simp-lee/isdict-api/internal/api/middleware"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/model"
)

func TestSearchWordsHTTP_SuccessEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type contextKey string
	const requestContextKey contextKey = "request-context-key"
	const requestContextValue = "request-context-value"

	var receivedCtx context.Context
	var receivedKeyword string
	var receivedCEFRLevel *int
	var receivedLimit int
	var receivedOffset int

	service := wordHandlerHTTPStubService{
		searchWordsFunc: func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
			receivedCtx = ctx
			receivedKeyword = keyword
			if cefrLevel != nil {
				level := *cefrLevel
				receivedCEFRLevel = &level
			}
			receivedLimit = limit
			receivedOffset = offset

			total := int64(1)
			return []model.SearchResultResponse{{Headword: "apple", WordAnnotations: model.WordAnnotations{CEFRLevel: "B2"}}}, &model.MetaInfo{Total: &total, Limit: &limit, Offset: &offset}, nil
		},
	}

	router := newWordHandlerHTTPTestRouter(service, &config.Config{APISearchMaxLimit: 50})
	requestContext := context.WithValue(context.Background(), requestContextKey, requestContextValue)
	resp, recorder := executeSuccessRequest(t, router, http.MethodGet, "/api/v1/search?q=apple&cefr_level=B1&limit=5&offset=3", nil, requestContext)
	if receivedCtx == nil {
		t.Fatal("expected request context to be passed to service")
	}
	if got := receivedCtx.Value(requestContextKey); got != requestContextValue {
		t.Fatalf("context value = %v, want %q", got, requestContextValue)
	}
	if receivedKeyword != "apple" {
		t.Fatalf("keyword = %q, want %q", receivedKeyword, "apple")
	}
	if receivedCEFRLevel == nil || *receivedCEFRLevel != 3 {
		t.Fatalf("cefrLevel = %v, want %d", receivedCEFRLevel, 3)
	}
	if receivedLimit != 5 {
		t.Fatalf("limit = %d, want %d", receivedLimit, 5)
	}
	if receivedOffset != 3 {
		t.Fatalf("offset = %d, want %d", receivedOffset, 3)
	}
	if requestID := recorder.Header().Get("X-Request-Id"); requestID == "" {
		t.Fatal("expected X-Request-Id header to be present")
	}
	data := assertSuccessArray(t, resp)
	first := mustObject(t, data[0], "data[0]")
	if got := first["headword"]; got != "apple" {
		t.Fatalf("headword = %#v, want %q", got, "apple")
	}
	meta := mustObject(t, resp["meta"], "meta")
	if got := meta["total"]; got != float64(1) {
		t.Fatalf("meta.total = %#v, want %v", got, float64(1))
	}
	if got := meta["limit"]; got != float64(5) {
		t.Fatalf("meta.limit = %#v, want %v", got, float64(5))
	}
	if got := meta["offset"]; got != float64(3) {
		t.Fatalf("meta.offset = %#v, want %v", got, float64(3))
	}
}

func TestSearchAndSuggestHTTP_QueryLengthValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		path        string
		wantCode    string
		wantMessage string
		wantDetails map[string]any
		service     wordHandlerHTTPStubService
	}{
		{
			name:        "search query too short",
			path:        "/api/v1/search?q=ab",
			wantCode:    "INVALID_PARAMETER",
			wantMessage: "Query must be at least 3 characters",
			wantDetails: map[string]any{
				"min_length": float64(MinQueryLength),
				"provided":   float64(2),
			},
			service: wordHandlerHTTPStubService{
				searchWordsFunc: func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
					t.Fatal("expected search validation to fail before service invocation")
					return nil, nil, nil
				},
			},
		},
		{
			name:        "search query too short after normalization",
			path:        "/api/v1/search?q=a-b",
			wantCode:    "INVALID_PARAMETER",
			wantMessage: "Query must be at least 3 characters",
			wantDetails: map[string]any{
				"min_length": float64(MinQueryLength),
				"provided":   float64(2),
			},
			service: wordHandlerHTTPStubService{
				searchWordsFunc: func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
					t.Fatal("expected normalized search validation to fail before service invocation")
					return nil, nil, nil
				},
			},
		},
		{
			name:        "suggest prefix too short",
			path:        "/api/v1/suggest?prefix=ab",
			wantCode:    "INVALID_PARAMETER",
			wantMessage: "Prefix must be at least 3 characters",
			wantDetails: map[string]any{
				"min_length": float64(MinQueryLength),
				"provided":   float64(2),
			},
			service: wordHandlerHTTPStubService{
				suggestWordsFunc: func(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
					t.Fatal("expected suggest validation to fail before service invocation")
					return nil, nil
				},
			},
		},
		{
			name:        "suggest prefix too short after normalization",
			path:        "/api/v1/suggest?prefix=a_a",
			wantCode:    "INVALID_PARAMETER",
			wantMessage: "Prefix must be at least 3 characters",
			wantDetails: map[string]any{
				"min_length": float64(MinQueryLength),
				"provided":   float64(2),
			},
			service: wordHandlerHTTPStubService{
				suggestWordsFunc: func(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
					t.Fatal("expected normalized suggest validation to fail before service invocation")
					return nil, nil
				},
			},
		},
		{
			name:        "search query too long",
			path:        "/api/v1/search?q=" + strings.Repeat("a", SearchQueryMaxLength+1),
			wantCode:    "INVALID_PARAMETER",
			wantMessage: "Query must not exceed 100 characters",
			wantDetails: map[string]any{
				"max_length": float64(SearchQueryMaxLength),
				"provided":   float64(SearchQueryMaxLength + 1),
			},
			service: wordHandlerHTTPStubService{
				searchWordsFunc: func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
					t.Fatal("expected overlong search query to fail before service invocation")
					return nil, nil, nil
				},
			},
		},
		{
			name:        "suggest prefix too long",
			path:        "/api/v1/suggest?prefix=" + strings.Repeat("a", SuggestPrefixMaxLength+1),
			wantCode:    "INVALID_PARAMETER",
			wantMessage: "Prefix must not exceed 50 characters",
			wantDetails: map[string]any{
				"max_length": float64(SuggestPrefixMaxLength),
				"provided":   float64(SuggestPrefixMaxLength + 1),
			},
			service: wordHandlerHTTPStubService{
				suggestWordsFunc: func(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
					t.Fatal("expected overlong suggest prefix to fail before service invocation")
					return nil, nil
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newWordHandlerHTTPTestRouter(tt.service, &config.Config{APISearchMaxLimit: 50, APISuggestMaxLimit: 20})
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
			}

			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			assertHTTPError(t, resp, tt.wantCode, tt.wantMessage)

			errObj := mustObject(t, resp["error"], "error")
			details := mustObject(t, errObj["details"], "error.details")
			for key, want := range tt.wantDetails {
				if got := details[key]; got != want {
					t.Fatalf("details[%q] = %#v, want %#v", key, got, want)
				}
			}
		})
	}
}

func TestGetWordsBatchHTTP_MalformedJSONReturnsStableMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := newWordHandlerHTTPTestRouter(wordHandlerHTTPStubService{
		getWordsBatchFunc: func(ctx context.Context, req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error) {
			t.Fatal("expected malformed JSON to fail before service invocation")
			return nil, nil, nil
		},
	}, &config.Config{APIBatchMaxSize: 100})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/words/batch", bytes.NewBufferString(`{"words":["apple"`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	assertHTTPError(t, resp, "INVALID_PARAMETER", invalidRequestBodyMessage)
	if strings.Contains(w.Body.String(), "unexpected EOF") {
		t.Fatalf("response body leaked decoder detail: %s", w.Body.String())
	}
}

func TestSearchPhrasesHTTP_QueryTooLong(t *testing.T) {
	gin.SetMode(gin.TestMode)

	service := wordHandlerHTTPStubService{
		searchPhrasesFunc: func(ctx context.Context, keyword string, limit int) ([]model.SuggestResponse, error) {
			t.Fatal("expected phrase validation to fail before service invocation")
			return nil, nil
		},
	}

	router := newWordHandlerHTTPTestRouter(service, &config.Config{APISearchMaxLimit: 50, APISuggestMaxLimit: 20})
	longQuery := strings.Repeat("a", 51)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/phrases?q="+longQuery, nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	assertHTTPError(t, resp, "INVALID_PARAMETER", "Keyword must not exceed 50 characters")
}

func TestSearchWordsHTTP_InvalidParameter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	serviceCalled := false
	service := wordHandlerHTTPStubService{
		searchWordsFunc: func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
			serviceCalled = true
			return nil, nil, nil
		},
	}

	router := newWordHandlerHTTPTestRouter(service, &config.Config{APISearchMaxLimit: 50})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=apple&cefr_level=3", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if serviceCalled {
		t.Fatal("expected validation failure before service invocation")
	}

	var resp struct {
		Success bool             `json:"success"`
		Error   *model.ErrorInfo `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Success {
		t.Fatal("expected success to be false")
	}
	if resp.Error == nil {
		t.Fatal("expected error payload")
	}
	if resp.Error.Code != "INVALID_PARAMETER" {
		t.Fatalf("error.code = %q, want %q", resp.Error.Code, "INVALID_PARAMETER")
	}
	if resp.Error.Message != "cefr_level must be one of: A1, A2, B1, B2, C1, C2" {
		t.Fatalf("error.message = %q, want exact validation message", resp.Error.Message)
	}
}

func TestSearchWordsHTTP_InternalErrorIsSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	serviceErr := errors.New("database connection reset")
	service := wordHandlerHTTPStubService{
		searchWordsFunc: func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
			return nil, nil, serviceErr
		},
	}

	router := newWordHandlerHTTPTestRouter(service, &config.Config{APISearchMaxLimit: 50})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=apple", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
	if requestID := w.Header().Get("X-Request-Id"); requestID == "" {
		t.Fatal("expected X-Request-Id header to be present")
	}

	var resp struct {
		Success bool             `json:"success"`
		Error   *model.ErrorInfo `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Success {
		t.Fatal("expected success to be false")
	}
	if resp.Error == nil {
		t.Fatal("expected error payload")
	}
	if resp.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("error.code = %q, want %q", resp.Error.Code, "INTERNAL_ERROR")
	}
	if resp.Error.Message != internalErrorMessage {
		t.Fatalf("error.message = %q, want %q", resp.Error.Message, internalErrorMessage)
	}
	if strings.Contains(w.Body.String(), serviceErr.Error()) {
		t.Fatalf("response body leaked internal error: %s", w.Body.String())
	}
}

func TestWordHandlerHTTP_NotFoundMappings(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		path        string
		service     wordHandlerHTTPStubService
		wantMessage string
	}{
		{
			name: "get word",
			path: "/api/v1/words/apple",
			service: wordHandlerHTTPStubService{
				getWordByHeadwordFunc: func(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error) {
					return nil, contracts.ErrWordNotFound
				},
			},
			wantMessage: "Word 'apple' not found in dictionary",
		},
		{
			name: "get word by variant",
			path: "/api/v1/words/by-variant/running",
			service: wordHandlerHTTPStubService{
				getWordsByVariantFunc: func(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error) {
					return nil, contracts.ErrVariantNotFound
				},
			},
			wantMessage: "Variant 'running' not found in dictionary",
		},
		{
			name: "get pronunciations",
			path: "/api/v1/words/apple/pronunciations",
			service: wordHandlerHTTPStubService{
				getPronunciationsFunc: func(ctx context.Context, headword string, accentCode *int) ([]model.PronunciationResponse, error) {
					return nil, contracts.ErrWordNotFound
				},
			},
			wantMessage: "Word 'apple' not found in dictionary",
		},
		{
			name: "get senses",
			path: "/api/v1/words/apple/senses",
			service: wordHandlerHTTPStubService{
				getSensesFunc: func(ctx context.Context, headword string, posCode *int, lang string) ([]model.SenseResponse, error) {
					return nil, contracts.ErrWordNotFound
				},
			},
			wantMessage: "Word 'apple' not found in dictionary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newWordHandlerHTTPTestRouter(tt.service, &config.Config{APISearchMaxLimit: 50, APISuggestMaxLimit: 20, APIBatchMaxSize: 100})
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusNotFound, w.Body.String())
			}

			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			assertHTTPError(t, resp, "WORD_NOT_FOUND", tt.wantMessage)
		})
	}
}

func TestWordHandlerHTTP_InvalidParameterMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		path        string
		wantMessage string
	}{
		{
			name:        "get word invalid accent",
			path:        "/api/v1/words/apple?accent=mars",
			wantMessage: "Invalid accent parameter",
		},
		{
			name:        "get word invalid include_variants",
			path:        "/api/v1/words/apple?include_variants=maybe",
			wantMessage: "include_variants must be 'true' or 'false'",
		},
		{
			name:        "get word by variant invalid include_pronunciations",
			path:        "/api/v1/words/by-variant/running?include_pronunciations=maybe",
			wantMessage: "include_pronunciations must be 'true' or 'false'",
		},
		{
			name:        "search invalid pos",
			path:        "/api/v1/search?q=apple&pos=thing",
			wantMessage: "Invalid pos parameter",
		},
		{
			name:        "search invalid oxford level",
			path:        "/api/v1/search?q=apple&oxford_level=3",
			wantMessage: "oxford_level must be 0 (any), 1 (Oxford 3000), or 2 (Oxford 5000)",
		},
		{
			name:        "suggest invalid cet level",
			path:        "/api/v1/suggest?prefix=apple&cet_level=5",
			wantMessage: "cet_level must be 0 (any), 4 (CET-4), or 6 (CET-6)",
		},
		{
			name:        "pronunciations invalid accent",
			path:        "/api/v1/words/apple/pronunciations?accent=mars",
			wantMessage: "Invalid accent parameter",
		},
		{
			name:        "senses invalid pos",
			path:        "/api/v1/words/apple/senses?pos=thing",
			wantMessage: "Invalid pos parameter",
		},
		{
			name:        "senses invalid lang",
			path:        "/api/v1/words/apple/senses?lang=jp",
			wantMessage: "lang must be one of 'both', 'en', or 'zh'",
		},
	}

	service := wordHandlerHTTPStubService{
		getWordByHeadwordFunc: func(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error) {
			t.Fatal("expected validation to fail before GetWordByHeadword invocation")
			return nil, nil
		},
		getWordsByVariantFunc: func(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error) {
			t.Fatal("expected validation to fail before GetWordsByVariant invocation")
			return nil, nil
		},
		searchWordsFunc: func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
			t.Fatal("expected validation to fail before SearchWords invocation")
			return nil, nil, nil
		},
		suggestWordsFunc: func(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
			t.Fatal("expected validation to fail before SuggestWords invocation")
			return nil, nil
		},
		getPronunciationsFunc: func(ctx context.Context, headword string, accentCode *int) ([]model.PronunciationResponse, error) {
			t.Fatal("expected validation to fail before GetPronunciations invocation")
			return nil, nil
		},
		getSensesFunc: func(ctx context.Context, headword string, posCode *int, lang string) ([]model.SenseResponse, error) {
			t.Fatal("expected validation to fail before GetSenses invocation")
			return nil, nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newWordHandlerHTTPTestRouter(service, &config.Config{APISearchMaxLimit: 50, APISuggestMaxLimit: 20, APIBatchMaxSize: 100})
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
			}

			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			assertHTTPError(t, resp, "INVALID_PARAMETER", tt.wantMessage)
		})
	}
}

func TestWordHandlerHTTP_TrimmedEnumParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		method  string
		path    string
		service wordHandlerHTTPStubService
	}{
		{
			name:   "get word trims accent",
			method: http.MethodGet,
			path:   "/api/v1/words/apple?accent=%20british%20",
			service: wordHandlerHTTPStubService{
				getWordByHeadwordFunc: func(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error) {
					if accentCode == nil || *accentCode != 1 {
						t.Fatalf("accentCode = %v, want %d", accentCode, 1)
					}
					return &model.WordResponse{Headword: headword}, nil
				},
			},
		},
		{
			name:   "search trims pos",
			method: http.MethodGet,
			path:   "/api/v1/search?q=apple&pos=%20noun%20",
			service: wordHandlerHTTPStubService{
				searchWordsFunc: func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
					if posCode == nil || *posCode != 1 {
						t.Fatalf("posCode = %v, want %d", posCode, 1)
					}
					return []model.SearchResultResponse{{Headword: keyword}}, nil, nil
				},
			},
		},
		{
			name:   "get senses trims lang",
			method: http.MethodGet,
			path:   "/api/v1/words/apple/senses?lang=%20en%20",
			service: wordHandlerHTTPStubService{
				getSensesFunc: func(ctx context.Context, headword string, posCode *int, lang string) ([]model.SenseResponse, error) {
					if lang != "en" {
						t.Fatalf("lang = %q, want %q", lang, "en")
					}
					return []model.SenseResponse{{SenseID: 1, DefinitionEN: "fruit"}}, nil
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newWordHandlerHTTPTestRouter(tt.service, &config.Config{
				APISearchMaxLimit:  50,
				APISuggestMaxLimit: 20,
				APIBatchMaxSize:    100,
			})

			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
			}
		})
	}
}

func TestWordHandlerHTTP_TrimmedBooleanParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		path   string
		assert func(t *testing.T, includeVariants, includePronunciations, includeSenses bool)
		stub   wordHandlerHTTPStubService
	}{
		{
			name: "get word trims boolean query params",
			path: "/api/v1/words/apple?include_variants=%20false%20&include_pronunciations=%20true%20&include_senses=%20false%20",
			assert: func(t *testing.T, includeVariants, includePronunciations, includeSenses bool) {
				if includeVariants {
					t.Fatal("includeVariants = true, want false")
				}
				if !includePronunciations {
					t.Fatal("includePronunciations = false, want true")
				}
				if includeSenses {
					t.Fatal("includeSenses = true, want false")
				}
			},
			stub: wordHandlerHTTPStubService{
				getWordByHeadwordFunc: func(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error) {
					if headword != "apple" {
						t.Fatalf("headword = %q, want %q", headword, "apple")
					}
					if includeVariants {
						t.Fatal("includeVariants = true, want false")
					}
					if !includePronunciations {
						t.Fatal("includePronunciations = false, want true")
					}
					if includeSenses {
						t.Fatal("includeSenses = true, want false")
					}
					return &model.WordResponse{Headword: headword}, nil
				},
			},
		},
		{
			name: "get word by variant trims boolean query params",
			path: "/api/v1/words/by-variant/running?include_pronunciations=%20false%20&include_senses=%20true%20",
			stub: wordHandlerHTTPStubService{
				getWordsByVariantFunc: func(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error) {
					if variant != "running" {
						t.Fatalf("variant = %q, want %q", variant, "running")
					}
					if includePronunciations {
						t.Fatal("includePronunciations = true, want false")
					}
					if !includeSenses {
						t.Fatal("includeSenses = false, want true")
					}
					return []model.VariantReverseResponse{{ID: 1, Headword: "run"}}, nil
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newWordHandlerHTTPTestRouter(tt.stub, &config.Config{
				APISearchMaxLimit:  50,
				APISuggestMaxLimit: 20,
				APIBatchMaxSize:    100,
			})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
			}
		})
	}
}

func TestWordHandlerHTTP_TrimmedPathParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		path string
		stub wordHandlerHTTPStubService
	}{
		{
			name: "get word trims headword path parameter",
			path: "/api/v1/words/%20apple%20",
			stub: wordHandlerHTTPStubService{
				getWordByHeadwordFunc: func(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error) {
					if headword != "apple" {
						t.Fatalf("headword = %q, want %q", headword, "apple")
					}
					return &model.WordResponse{Headword: headword}, nil
				},
			},
		},
		{
			name: "get word by variant trims variant path parameter",
			path: "/api/v1/words/by-variant/%20running%20",
			stub: wordHandlerHTTPStubService{
				getWordsByVariantFunc: func(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error) {
					if variant != "running" {
						t.Fatalf("variant = %q, want %q", variant, "running")
					}
					return []model.VariantReverseResponse{{ID: 1, Headword: "run"}}, nil
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newWordHandlerHTTPTestRouter(tt.stub, &config.Config{
				APISearchMaxLimit:  50,
				APISuggestMaxLimit: 20,
				APIBatchMaxSize:    100,
			})

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
			}
		})
	}
}

func TestWordHandlerHTTP_TrimmedNumericParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		method  string
		path    string
		service wordHandlerHTTPStubService
	}{
		{
			name:   "search trims numeric filters",
			method: http.MethodGet,
			path:   "/api/v1/search?q=apple&oxford_level=%201%20&cet_level=%206%20&max_frequency_rank=%20100%20&min_collins_stars=%202%20&limit=%205%20&offset=%203%20",
			service: wordHandlerHTTPStubService{
				searchWordsFunc: func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
					assertTrimmedSearchNumbers(t, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit, offset)
					return []model.SearchResultResponse{{Headword: keyword}}, &model.MetaInfo{}, nil
				},
			},
		},
		{
			name:   "suggest trims numeric filters",
			method: http.MethodGet,
			path:   "/api/v1/suggest?prefix=apple&oxford_level=%202%20&cet_level=%204%20&max_frequency_rank=%20150%20&min_collins_stars=%204%20&limit=%207%20",
			service: wordHandlerHTTPStubService{
				suggestWordsFunc: func(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
					assertTrimmedSuggestNumbers(t, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit)
					return []model.SuggestResponse{{Headword: prefix}}, nil
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newWordHandlerHTTPTestRouter(tt.service, &config.Config{APISearchMaxLimit: 50, APISuggestMaxLimit: 20})
			_, _ = executeSuccessRequest(t, router, tt.method, tt.path, nil, context.Background())
		})
	}
}

func assertTrimmedSearchNumbers(t *testing.T, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars *int, limit, offset int) {
	t.Helper()
	if oxfordLevel == nil || *oxfordLevel != 1 || cetLevel == nil || *cetLevel != 2 || maxFrequencyRank == nil || *maxFrequencyRank != 100 || minCollinsStars == nil || *minCollinsStars != 2 || limit != 5 || offset != 3 {
		t.Fatalf("unexpected trimmed search params: oxford=%v cet=%v max=%v collins=%v limit=%d offset=%d", oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit, offset)
	}
}

func assertTrimmedSuggestNumbers(t *testing.T, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars *int, limit int) {
	t.Helper()
	if oxfordLevel == nil || *oxfordLevel != 2 || cetLevel == nil || *cetLevel != 1 || maxFrequencyRank == nil || *maxFrequencyRank != 150 || minCollinsStars == nil || *minCollinsStars != 4 || limit != 7 {
		t.Fatalf("unexpected trimmed suggest params: oxford=%v cet=%v max=%v collins=%v limit=%d", oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit)
	}
}

func TestSearchAndSuggestHTTP_OxfordLevelZeroMeansAny(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		path string
		stub wordHandlerHTTPStubService
	}{
		{
			name: "search omits oxford filter for zero",
			path: "/api/v1/search?q=apple&oxford_level=0",
			stub: wordHandlerHTTPStubService{
				searchWordsFunc: func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
					if oxfordLevel != nil {
						t.Fatalf("oxfordLevel = %v, want nil", *oxfordLevel)
					}
					return []model.SearchResultResponse{{Headword: keyword}}, &model.MetaInfo{}, nil
				},
			},
		},
		{
			name: "suggest omits oxford filter for zero",
			path: "/api/v1/suggest?prefix=apple&oxford_level=0",
			stub: wordHandlerHTTPStubService{
				suggestWordsFunc: func(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
					if oxfordLevel != nil {
						t.Fatalf("oxfordLevel = %v, want nil", *oxfordLevel)
					}
					return []model.SuggestResponse{{Headword: prefix}}, nil
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newWordHandlerHTTPTestRouter(tt.stub, &config.Config{APISearchMaxLimit: 50, APISuggestMaxLimit: 20})
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
			}
		})
	}
}

func TestWordHandlerHTTP_SuccessEnvelopes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	total := int64(1)
	offset := 0

	tests := []struct {
		name       string
		method     string
		path       string
		body       []byte
		service    wordHandlerHTTPStubService
		assertBody func(t *testing.T, resp map[string]any)
	}{
		{
			name:   "get word success envelope",
			method: http.MethodGet,
			path:   "/api/v1/words/apple",
			service: wordHandlerHTTPStubService{
				getWordByHeadwordFunc: func(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error) {
					return &model.WordResponse{ID: 1, Headword: headword}, nil
				},
			},
			assertBody: func(t *testing.T, resp map[string]any) {
				data := assertSuccessObject(t, resp)
				if got := data["headword"]; got != "apple" {
					t.Fatalf("data.headword = %#v, want %q", got, "apple")
				}
			},
		},
		{
			name:   "get word by variant success envelope",
			method: http.MethodGet,
			path:   "/api/v1/words/by-variant/running",
			service: wordHandlerHTTPStubService{
				getWordsByVariantFunc: func(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error) {
					return []model.VariantReverseResponse{{ID: 2, Headword: "run"}}, nil
				},
			},
			assertBody: func(t *testing.T, resp map[string]any) {
				data := assertSuccessArray(t, resp)
				first := mustObject(t, data[0], "data[0]")
				if got := first["headword"]; got != "run" {
					t.Fatalf("data[0].headword = %#v, want %q", got, "run")
				}
			},
		},
		{
			name:   "batch success envelope",
			method: http.MethodPost,
			path:   "/api/v1/words/batch",
			body:   []byte(`{"words":["apple"]}`),
			service: wordHandlerHTTPStubService{
				getWordsBatchFunc: func(ctx context.Context, req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error) {
					limit := len(req.Words)
					return []model.WordResponse{{ID: 1, Headword: req.Words[0]}}, &model.MetaInfo{Total: &total, Limit: &limit, Offset: &offset}, nil
				},
			},
			assertBody: func(t *testing.T, resp map[string]any) {
				data := assertSuccessArray(t, resp)
				first := mustObject(t, data[0], "data[0]")
				if got := first["headword"]; got != "apple" {
					t.Fatalf("data[0].headword = %#v, want %q", got, "apple")
				}
				meta := mustObject(t, resp["meta"], "meta")
				if got := meta["total"]; got != float64(1) {
					t.Fatalf("meta.total = %#v, want %v", got, float64(1))
				}
			},
		},
		{
			name:   "suggest success envelope",
			method: http.MethodGet,
			path:   "/api/v1/suggest?prefix=app&cefr_level=B1",
			service: wordHandlerHTTPStubService{
				suggestWordsFunc: func(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
					if cefrLevel == nil || *cefrLevel != 3 {
						t.Fatalf("cefrLevel = %v, want %d", cefrLevel, 3)
					}
					return []model.SuggestResponse{{Headword: "apple"}}, nil
				},
			},
			assertBody: func(t *testing.T, resp map[string]any) {
				data := assertSuccessArray(t, resp)
				first := mustObject(t, data[0], "data[0]")
				if got := first["headword"]; got != "apple" {
					t.Fatalf("data[0].headword = %#v, want %q", got, "apple")
				}
			},
		},
		{
			name:   "phrases success envelope",
			method: http.MethodGet,
			path:   "/api/v1/phrases?q=carry%20on",
			service: wordHandlerHTTPStubService{
				searchPhrasesFunc: func(ctx context.Context, keyword string, limit int) ([]model.SuggestResponse, error) {
					return []model.SuggestResponse{{Headword: keyword}}, nil
				},
			},
			assertBody: func(t *testing.T, resp map[string]any) {
				data := assertSuccessArray(t, resp)
				first := mustObject(t, data[0], "data[0]")
				if got := first["headword"]; got != "carry on" {
					t.Fatalf("data[0].headword = %#v, want %q", got, "carry on")
				}
			},
		},
		{
			name:   "pronunciations success envelope",
			method: http.MethodGet,
			path:   "/api/v1/words/apple/pronunciations",
			service: wordHandlerHTTPStubService{
				getPronunciationsFunc: func(ctx context.Context, headword string, accentCode *int) ([]model.PronunciationResponse, error) {
					return []model.PronunciationResponse{{Accent: "american", IPA: "ˈæpəl", IsPrimary: true}}, nil
				},
			},
			assertBody: func(t *testing.T, resp map[string]any) {
				data := assertSuccessArray(t, resp)
				first := mustObject(t, data[0], "data[0]")
				if got := first["accent"]; got != "american" {
					t.Fatalf("data[0].accent = %#v, want %q", got, "american")
				}
			},
		},
		{
			name:   "senses success envelope",
			method: http.MethodGet,
			path:   "/api/v1/words/apple/senses?lang=en",
			service: wordHandlerHTTPStubService{
				getSensesFunc: func(ctx context.Context, headword string, posCode *int, lang string) ([]model.SenseResponse, error) {
					return []model.SenseResponse{{SenseID: 11, POS: "noun", CEFRLevel: "B2", DefinitionEN: "fruit", SenseOrder: 1}}, nil
				},
			},
			assertBody: func(t *testing.T, resp map[string]any) {
				data := assertSuccessArray(t, resp)
				first := mustObject(t, data[0], "data[0]")
				if got := first["definition_en"]; got != "fruit" {
					t.Fatalf("data[0].definition_en = %#v, want %q", got, "fruit")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newWordHandlerHTTPTestRouter(tt.service, &config.Config{APISearchMaxLimit: 50, APISuggestMaxLimit: 20, APIBatchMaxSize: 100})
			resp, _ := executeSuccessRequest(t, router, tt.method, tt.path, tt.body, context.Background())
			tt.assertBody(t, resp)
		})
	}
}

func executeSuccessRequest(t *testing.T, router *gin.Engine, method, path string, body []byte, requestContext context.Context) (map[string]any, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body)).WithContext(requestContext)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if success, ok := resp["success"].(bool); !ok || !success {
		t.Fatalf("success = %#v, want true", resp["success"])
	}
	return resp, w
}

func newWordHandlerHTTPTestRouter(service WordServiceInterface, cfg *config.Config) *gin.Engine {
	router := gin.New()
	router.Use(middleware.SetupMiddleware(&config.Config{EnableRequestID: true}))

	handler := NewWordHandler(service, cfg)
	router.GET("/api/v1/words/by-variant/:variant", handler.GetWordByVariant)
	router.POST("/api/v1/words/batch", handler.GetWordsBatch)
	router.GET("/api/v1/search", handler.SearchWords)
	router.GET("/api/v1/suggest", handler.SuggestWords)
	router.GET("/api/v1/phrases", handler.SearchPhrases)
	router.GET("/api/v1/words/:headword/pronunciations", handler.GetPronunciations)
	router.GET("/api/v1/words/:headword/senses", handler.GetSenses)
	router.GET("/api/v1/words/:headword", handler.GetWord)

	return router
}

type wordHandlerHTTPStubService struct {
	getWordByHeadwordFunc func(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error)
	getWordsByVariantFunc func(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error)
	getWordsBatchFunc     func(ctx context.Context, req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error)
	searchWordsFunc       func(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error)
	suggestWordsFunc      func(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error)
	searchPhrasesFunc     func(ctx context.Context, keyword string, limit int) ([]model.SuggestResponse, error)
	getPronunciationsFunc func(ctx context.Context, headword string, accentCode *int) ([]model.PronunciationResponse, error)
	getSensesFunc         func(ctx context.Context, headword string, posCode *int, lang string) ([]model.SenseResponse, error)
}

func (s wordHandlerHTTPStubService) GetWordByHeadword(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error) {
	if s.getWordByHeadwordFunc == nil {
		return nil, fmt.Errorf("unexpected GetWordByHeadword call")
	}
	return s.getWordByHeadwordFunc(ctx, headword, accentCode, includeVariants, includePronunciations, includeSenses)
}

func (s wordHandlerHTTPStubService) GetWordsByVariant(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error) {
	if s.getWordsByVariantFunc == nil {
		return nil, fmt.Errorf("unexpected GetWordsByVariant call")
	}
	return s.getWordsByVariantFunc(ctx, variant, kindStr, includePronunciations, includeSenses)
}

func (s wordHandlerHTTPStubService) GetWordsBatch(ctx context.Context, req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error) {
	if s.getWordsBatchFunc == nil {
		return nil, nil, fmt.Errorf("unexpected GetWordsBatch call")
	}
	return s.getWordsBatchFunc(ctx, req)
}

func (s wordHandlerHTTPStubService) SearchWords(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
	if s.searchWordsFunc == nil {
		return nil, nil, fmt.Errorf("unexpected SearchWords call")
	}
	return s.searchWordsFunc(ctx, keyword, posCode, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit, offset)
}

func (s wordHandlerHTTPStubService) SuggestWords(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
	if s.suggestWordsFunc == nil {
		return nil, fmt.Errorf("unexpected SuggestWords call")
	}
	return s.suggestWordsFunc(ctx, prefix, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit)
}

func (s wordHandlerHTTPStubService) SearchPhrases(ctx context.Context, keyword string, limit int) ([]model.SuggestResponse, error) {
	if s.searchPhrasesFunc == nil {
		return nil, fmt.Errorf("unexpected SearchPhrases call")
	}
	return s.searchPhrasesFunc(ctx, keyword, limit)
}

func (s wordHandlerHTTPStubService) GetPronunciations(ctx context.Context, headword string, accentCode *int) ([]model.PronunciationResponse, error) {
	if s.getPronunciationsFunc == nil {
		return nil, fmt.Errorf("unexpected GetPronunciations call")
	}
	return s.getPronunciationsFunc(ctx, headword, accentCode)
}

func (s wordHandlerHTTPStubService) GetSenses(ctx context.Context, headword string, posCode *int, lang string) ([]model.SenseResponse, error) {
	if s.getSensesFunc == nil {
		return nil, fmt.Errorf("unexpected GetSenses call")
	}
	return s.getSensesFunc(ctx, headword, posCode, lang)
}

func assertHTTPError(t *testing.T, resp map[string]any, wantCode, wantMessage string) {
	t.Helper()

	if success, ok := resp["success"].(bool); !ok || success {
		t.Fatalf("success = %#v, want false", resp["success"])
	}

	errObj := mustObject(t, resp["error"], "error")
	if got := errObj["code"]; got != wantCode {
		t.Fatalf("error.code = %#v, want %q", got, wantCode)
	}
	if got := errObj["message"]; got != wantMessage {
		t.Fatalf("error.message = %#v, want %q", got, wantMessage)
	}
}

func assertSuccessObject(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()

	return mustObject(t, resp["data"], "data")
}

func assertSuccessArray(t *testing.T, resp map[string]any) []any {
	t.Helper()

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatalf("data = %#v, want array", resp["data"])
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty data array")
	}
	return data
}

func mustObject(t *testing.T, value any, field string) map[string]any {
	t.Helper()

	obj, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", field, value)
	}
	return obj
}
