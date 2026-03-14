package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/simp-lee/isdict-api/internal/api/contracts"
	"github.com/simp-lee/isdict-api/internal/api/queryvalidation"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/ginx"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/model"
)

// WordServiceInterface defines the minimal service contract the handler needs.
// Implementations should return contracts.ErrWordNotFound or contracts.ErrVariantNotFound
// when the requested resource does not exist.
type WordServiceInterface interface {
	GetWordByHeadword(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error)
	GetWordsByVariant(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error)
	GetWordsBatch(ctx context.Context, req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error)
	SearchWords(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error)
	SuggestWords(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error)
	SearchPhrases(ctx context.Context, keyword string, limit int) ([]model.SuggestResponse, error)
	GetPronunciations(ctx context.Context, headword string, accentCode *int) ([]model.PronunciationResponse, error)
	GetSenses(ctx context.Context, headword string, posCode *int, lang string) ([]model.SenseResponse, error)
}

// WordHandler handles HTTP requests for word operations
type WordHandler struct {
	service WordServiceInterface
	config  *config.Config
}

// NewWordHandler creates a new word handler instance
func NewWordHandler(service WordServiceInterface, cfg *config.Config) *WordHandler {
	return &WordHandler{
		service: service,
		config:  cfg,
	}
}

// Helper functions for parameter parsing and validation

// MinQueryLength is the minimum length for search queries (in characters, not bytes)
const MinQueryLength = queryvalidation.MinQueryLength

const SearchQueryMaxLength = 100

const SuggestPrefixMaxLength = 50

const internalErrorMessage = "An internal error occurred"

const invalidRequestBodyMessage = "Invalid request body"

func writeInternalError(c *gin.Context, handlerName string, err error) {
	requestID, _ := ginx.GetRequestID(c)
	slog.Default().Error("handler request failed",
		"handler", handlerName,
		"request_id", requestID,
		"error", err,
	)

	c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
		"INTERNAL_ERROR",
		internalErrorMessage,
		nil,
	))
}

func trimmedQuery(c *gin.Context, paramName string) string {
	return strings.TrimSpace(c.Query(paramName))
}

func trimmedPathParam(c *gin.Context, paramName string) string {
	return strings.TrimSpace(c.Param(paramName))
}

func validateQueryLength(query, fieldName string, maxLength int) (string, map[string]interface{}) {
	normalizedLength := queryvalidation.NormalizedRuneCount(query)
	if normalizedLength < MinQueryLength {
		return fieldName + " must be at least " + strconv.Itoa(MinQueryLength) + " characters", map[string]interface{}{
			"min_length": MinQueryLength,
			"provided":   normalizedLength,
		}
	}

	length := queryvalidation.TrimmedRuneCount(query)
	if maxLength > 0 && length > maxLength {
		return fieldName + " must not exceed " + strconv.Itoa(maxLength) + " characters", map[string]interface{}{
			"max_length": maxLength,
			"provided":   length,
		}
	}

	return "", nil
}

// parseAccent parses and validates the accent query parameter
func parseAccent(c *gin.Context, paramName string) (*int, bool) {
	accentParam := trimmedQuery(c, paramName)
	if accentParam == "" {
		return nil, true
	}
	accentLower := strings.ToLower(accentParam)
	if code, ok := model.ParseAccent(accentLower); ok {
		return &code, true
	}
	c.JSON(http.StatusBadRequest, model.NewErrorResponse(
		"INVALID_PARAMETER",
		"Invalid accent parameter",
		nil,
	))
	return nil, false
}

// parsePOS parses and validates the POS query parameter
func parsePOS(c *gin.Context, paramName string) (*int, bool) {
	posParam := trimmedQuery(c, paramName)
	if posParam == "" {
		return nil, true
	}
	posLower := strings.ToLower(posParam)
	if code, ok := model.ParsePOS(posLower); ok {
		return &code, true
	}
	c.JSON(http.StatusBadRequest, model.NewErrorResponse(
		"INVALID_PARAMETER",
		"Invalid pos parameter",
		nil,
	))
	return nil, false
}

// parseCEFRLevel parses and validates the CEFR level query parameter
func parseCEFRLevel(c *gin.Context, paramName string) (*int, bool) {
	cefrParam := trimmedQuery(c, paramName)
	if cefrParam == "" {
		return nil, true
	}
	normalized := strings.ToUpper(cefrParam)
	if level, ok := model.ParseCEFRLevel(normalized); ok {
		return &level, true
	}
	c.JSON(http.StatusBadRequest, model.NewErrorResponse(
		"INVALID_PARAMETER",
		"cefr_level must be one of: A1, A2, B1, B2, C1, C2",
		nil,
	))
	return nil, false
}

// parseOxfordLevel parses and validates the Oxford level query parameter
func parseOxfordLevel(c *gin.Context, paramName string) (*int, bool) {
	oxfordParam := trimmedQuery(c, paramName)
	if oxfordParam == "" {
		return nil, true
	}
	level, err := strconv.Atoi(oxfordParam)
	if err != nil || level < 0 || level > 2 {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			"oxford_level must be 0 (any), 1 (Oxford 3000), or 2 (Oxford 5000)",
			nil,
		))
		return nil, false
	}
	if level == 0 {
		return nil, true
	}
	return &level, true
}

// parseCETLevel parses and validates the CET level query parameter
func parseCETLevel(c *gin.Context, paramName string) (*int, bool) {
	cetParam := trimmedQuery(c, paramName)
	if cetParam == "" {
		return nil, true
	}
	apiLevel, err := strconv.Atoi(cetParam)
	if err != nil || (apiLevel != 0 && apiLevel != 4 && apiLevel != 6) {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			"cet_level must be 0 (any), 4 (CET-4), or 6 (CET-6)",
			nil,
		))
		return nil, false
	}

	if apiLevel == 0 {
		return nil, true // 0 means no filtering
	}

	// Convert API level to DB level: 4->1, 6->2
	dbLevel := 1
	if apiLevel == 6 {
		dbLevel = 2
	}
	return &dbLevel, true
}

// parseMaxFrequencyRank parses and validates the max frequency rank query parameter
func parseMaxFrequencyRank(c *gin.Context, paramName string) (*int, bool) {
	rankParam := trimmedQuery(c, paramName)
	if rankParam == "" {
		return nil, true
	}
	rank, err := strconv.Atoi(rankParam)
	if err != nil || rank <= 0 {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			"max_frequency_rank must be a positive integer",
			nil,
		))
		return nil, false
	}
	return &rank, true
}

// parseMinCollinsStars parses and validates the minimum Collins stars query parameter
func parseMinCollinsStars(c *gin.Context, paramName string) (*int, bool) {
	starsParam := trimmedQuery(c, paramName)
	if starsParam == "" {
		return nil, true
	}
	stars, err := strconv.Atoi(starsParam)
	if err != nil || stars < 0 || stars > 5 {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			"min_collins_stars must be between 0 and 5",
			nil,
		))
		return nil, false
	}
	return &stars, true
}

// parseLimit parses and validates the limit query parameter
func parseLimit(c *gin.Context, defaultLimit, maxLimit int) (int, bool) {
	limitParam := trimmedQuery(c, "limit")
	if limitParam == "" {
		return defaultLimit, true
	}
	limit, err := strconv.Atoi(limitParam)
	if err != nil || limit <= 0 || limit > maxLimit {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			"limit must be between 1 and "+strconv.Itoa(maxLimit),
			nil,
		))
		return 0, false
	}
	return limit, true
}

// parseOffset parses and validates the offset query parameter
func parseOffset(c *gin.Context) (int, bool) {
	offsetParam := trimmedQuery(c, "offset")
	if offsetParam == "" {
		return 0, true
	}
	offset, err := strconv.Atoi(offsetParam)
	if err != nil || offset < 0 {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			"offset must be a non-negative integer",
			nil,
		))
		return 0, false
	}
	return offset, true
}

// parseBool parses and validates a boolean query parameter
func parseBool(c *gin.Context, paramName string, defaultValue bool) (bool, bool) {
	param := trimmedQuery(c, paramName)
	if param == "" {
		return defaultValue, true
	}
	switch strings.ToLower(param) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			paramName+" must be 'true' or 'false'",
			nil,
		))
		return false, false
	}
}

// validateHeadword validates the headword path parameter
func validateHeadword(c *gin.Context) (string, bool) {
	headword := trimmedPathParam(c, "headword")
	if headword == "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"MISSING_PARAMETER",
			"Headword parameter is required",
			nil,
		))
		return "", false
	}
	return headword, true
}

// GetWord handles GET /api/v1/words/:headword
func (h *WordHandler) GetWord(c *gin.Context) {
	headword, ok := validateHeadword(c)
	if !ok {
		return
	}

	accentCode, ok := parseAccent(c, "accent")
	if !ok {
		return
	}

	includeVariants, ok := parseBool(c, "include_variants", true)
	if !ok {
		return
	}

	includePronunciations, ok := parseBool(c, "include_pronunciations", true)
	if !ok {
		return
	}

	includeSenses, ok := parseBool(c, "include_senses", true)
	if !ok {
		return
	}

	word, err := h.service.GetWordByHeadword(c.Request.Context(), headword, accentCode, includeVariants, includePronunciations, includeSenses)
	if err != nil {
		if errors.Is(err, contracts.ErrWordNotFound) {
			c.JSON(http.StatusNotFound, model.NewErrorResponse(
				"WORD_NOT_FOUND",
				"Word '"+headword+"' not found in dictionary",
				nil,
			))
			return
		}
		writeInternalError(c, "GetWord", err)
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponse(word))
}

// GetWordByVariant handles GET /api/v1/words/by-variant/:variant
func (h *WordHandler) GetWordByVariant(c *gin.Context) {
	variant := trimmedPathParam(c, "variant")
	if variant == "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"MISSING_PARAMETER",
			"Variant parameter is required",
			nil,
		))
		return
	}

	var kind *string
	if kindParam := trimmedQuery(c, "kind"); kindParam != "" {
		kindLower := strings.ToLower(kindParam)
		if kindLower != "form" && kindLower != "alias" {
			c.JSON(http.StatusBadRequest, model.NewErrorResponse(
				"INVALID_PARAMETER",
				"Invalid kind parameter. Must be 'form' or 'alias'",
				nil,
			))
			return
		}
		kind = &kindLower
	}

	includePronunciations, ok := parseBool(c, "include_pronunciations", true)
	if !ok {
		return
	}

	includeSenses, ok := parseBool(c, "include_senses", true)
	if !ok {
		return
	}

	words, err := h.service.GetWordsByVariant(c.Request.Context(), variant, kind, includePronunciations, includeSenses)
	if err != nil {
		if errors.Is(err, contracts.ErrVariantNotFound) {
			c.JSON(http.StatusNotFound, model.NewErrorResponse(
				"WORD_NOT_FOUND",
				"Variant '"+variant+"' not found in dictionary",
				nil,
			))
			return
		}
		writeInternalError(c, "GetWordByVariant", err)
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponse(words))
}

// GetWordsBatch handles POST /api/v1/words/batch
func (h *WordHandler) GetWordsBatch(c *gin.Context) {
	var req model.BatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			invalidRequestBodyMessage,
			nil,
		))
		return
	}

	if len(req.Words) == 0 {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"MISSING_PARAMETER",
			"Request body must include at least one word",
			nil,
		))
		return
	}

	cleanedWords := queryvalidation.NormalizeBatchWords(req.Words)

	if len(cleanedWords) == 0 {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			"No valid words provided",
			nil,
		))
		return
	}

	if len(cleanedWords) > h.config.APIBatchMaxSize {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"BATCH_LIMIT_EXCEEDED",
			"Batch size exceeds the maximum limit of "+strconv.Itoa(h.config.APIBatchMaxSize)+" words",
			nil,
		))
		return
	}

	req.Words = cleanedWords

	words, meta, err := h.service.GetWordsBatch(c.Request.Context(), &req)
	if err != nil {
		writeInternalError(c, "GetWordsBatch", err)
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponseWithMeta(words, meta))
}

// SearchWords handles GET /api/v1/search
func (h *WordHandler) SearchWords(c *gin.Context) {
	keyword := trimmedQuery(c, "q")
	if keyword == "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"MISSING_PARAMETER",
			"Query parameter 'q' is required",
			nil,
		))
		return
	}

	if message, details := validateQueryLength(keyword, "Query", SearchQueryMaxLength); message != "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			message,
			details,
		))
		return
	}

	limit, ok := parseLimit(c, 20, h.config.APISearchMaxLimit)
	if !ok {
		return
	}

	offset, ok := parseOffset(c)
	if !ok {
		return
	}

	posCode, ok := parsePOS(c, "pos")
	if !ok {
		return
	}

	cefrLevel, ok := parseCEFRLevel(c, "cefr_level")
	if !ok {
		return
	}

	oxfordLevel, ok := parseOxfordLevel(c, "oxford_level")
	if !ok {
		return
	}

	cetLevel, ok := parseCETLevel(c, "cet_level")
	if !ok {
		return
	}

	maxFrequencyRank, ok := parseMaxFrequencyRank(c, "max_frequency_rank")
	if !ok {
		return
	}

	minCollinsStars, ok := parseMinCollinsStars(c, "min_collins_stars")
	if !ok {
		return
	}

	results, meta, err := h.service.SearchWords(c.Request.Context(), keyword, posCode, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit, offset)
	if err != nil {
		writeInternalError(c, "SearchWords", err)
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponseWithMeta(results, meta))
}

// SuggestWords handles GET /api/v1/suggest
func (h *WordHandler) SuggestWords(c *gin.Context) {
	prefix := trimmedQuery(c, "prefix")
	if prefix == "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"MISSING_PARAMETER",
			"Query parameter 'prefix' is required",
			nil,
		))
		return
	}

	if message, details := validateQueryLength(prefix, "Prefix", SuggestPrefixMaxLength); message != "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			message,
			details,
		))
		return
	}

	limit, ok := parseLimit(c, 10, h.config.APISuggestMaxLimit)
	if !ok {
		return
	}

	cefrLevel, ok := parseCEFRLevel(c, "cefr_level")
	if !ok {
		return
	}

	oxfordLevel, ok := parseOxfordLevel(c, "oxford_level")
	if !ok {
		return
	}

	cetLevel, ok := parseCETLevel(c, "cet_level")
	if !ok {
		return
	}

	maxFrequencyRank, ok := parseMaxFrequencyRank(c, "max_frequency_rank")
	if !ok {
		return
	}

	minCollinsStars, ok := parseMinCollinsStars(c, "min_collins_stars")
	if !ok {
		return
	}

	results, err := h.service.SuggestWords(c.Request.Context(), prefix, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit)
	if err != nil {
		writeInternalError(c, "SuggestWords", err)
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponse(results))
}

// SearchPhrases handles GET /api/v1/phrases
func (h *WordHandler) SearchPhrases(c *gin.Context) {
	keyword := c.Query("q")
	keyword = strings.TrimSpace(keyword)

	if keyword == "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"MISSING_PARAMETER",
			"Query parameter 'q' is required",
			nil,
		))
		return
	}

	// Validate keyword length (prevent excessive query overhead)
	keywordRunes := []rune(keyword)
	if len(keywordRunes) > 50 {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			"Keyword must not exceed 50 characters",
			nil,
		))
		return
	}

	limit, ok := parseLimit(c, 10, 50)
	if !ok {
		return
	}

	results, err := h.service.SearchPhrases(c.Request.Context(), keyword, limit)
	if err != nil {
		writeInternalError(c, "SearchPhrases", err)
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponse(results))
}

// GetPronunciations handles GET /api/v1/words/:headword/pronunciations
func (h *WordHandler) GetPronunciations(c *gin.Context) {
	headword, ok := validateHeadword(c)
	if !ok {
		return
	}

	accentCode, ok := parseAccent(c, "accent")
	if !ok {
		return
	}

	pronunciations, err := h.service.GetPronunciations(c.Request.Context(), headword, accentCode)
	if err != nil {
		if errors.Is(err, contracts.ErrWordNotFound) {
			c.JSON(http.StatusNotFound, model.NewErrorResponse(
				"WORD_NOT_FOUND",
				"Word '"+headword+"' not found in dictionary",
				nil,
			))
			return
		}
		writeInternalError(c, "GetPronunciations", err)
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponse(pronunciations))
}

// GetSenses handles GET /api/v1/words/:headword/senses
func (h *WordHandler) GetSenses(c *gin.Context) {
	headword, ok := validateHeadword(c)
	if !ok {
		return
	}

	posCode, ok := parsePOS(c, "pos")
	if !ok {
		return
	}

	lang := strings.ToLower(trimmedQuery(c, "lang"))
	if lang == "" {
		lang = "both"
	}
	switch lang {
	case "both", "en", "zh":
	default:
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			"lang must be one of 'both', 'en', or 'zh'",
			nil,
		))
		return
	}

	senses, err := h.service.GetSenses(c.Request.Context(), headword, posCode, lang)
	if err != nil {
		if errors.Is(err, contracts.ErrWordNotFound) {
			c.JSON(http.StatusNotFound, model.NewErrorResponse(
				"WORD_NOT_FOUND",
				"Word '"+headword+"' not found in dictionary",
				nil,
			))
			return
		}
		writeInternalError(c, "GetSenses", err)
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponse(senses))
}
