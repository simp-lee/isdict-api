package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/isdict-api/internal/api/repository"
	"github.com/simp-lee/isdict-api/internal/api/service"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/model"
)

// WordHandler handles HTTP requests for word operations
type WordHandler struct {
	service *service.WordService
	config  *config.Config
}

// NewWordHandler creates a new word handler instance
func NewWordHandler(service *service.WordService, cfg *config.Config) *WordHandler {
	return &WordHandler{
		service: service,
		config:  cfg,
	}
}

// Helper functions for parameter parsing and validation

// MinQueryLength is the minimum length for search queries (in characters, not bytes)
const MinQueryLength = 3

// validateQueryLength validates that the query meets the minimum length requirement
func validateQueryLength(query string) error {
	length := utf8.RuneCountInString(strings.TrimSpace(query))
	if length < MinQueryLength {
		return errors.New("查询长度至少需要 3 个字符")
	}
	return nil
}

// parseAccent parses and validates the accent query parameter
func parseAccent(c *gin.Context, paramName string) (*int, bool) {
	accentParam := c.Query(paramName)
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
	posParam := c.Query(paramName)
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
	cefrParam := c.Query(paramName)
	if cefrParam == "" {
		return nil, true
	}
	level, err := strconv.Atoi(cefrParam)
	if err != nil || level < 0 || level > 6 {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			"cefr_level must be between 0 and 6",
			nil,
		))
		return nil, false
	}
	return &level, true
}

// parseOxfordLevel parses and validates the Oxford level query parameter
func parseOxfordLevel(c *gin.Context, paramName string) (*int, bool) {
	oxfordParam := c.Query(paramName)
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
	return &level, true
}

// parseCETLevel parses and validates the CET level query parameter
func parseCETLevel(c *gin.Context, paramName string) (*int, bool) {
	cetParam := c.Query(paramName)
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
	rankParam := c.Query(paramName)
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
	starsParam := c.Query(paramName)
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
	limitParam := c.Query("limit")
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
	offsetParam := c.Query("offset")
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
	param := c.Query(paramName)
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
	headword := c.Param("headword")
	if strings.TrimSpace(headword) == "" {
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

	word, err := h.service.GetWordByHeadword(headword, accentCode, includeVariants, includePronunciations, includeSenses)
	if err != nil {
		if errors.Is(err, repository.ErrWordNotFound) {
			c.JSON(http.StatusNotFound, model.NewErrorResponse(
				"WORD_NOT_FOUND",
				"Word '"+headword+"' not found in dictionary",
				nil,
			))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
			"INTERNAL_ERROR",
			err.Error(),
			nil,
		))
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponse(word))
}

// GetWordByVariant handles GET /api/v1/words/by-variant/:variant
func (h *WordHandler) GetWordByVariant(c *gin.Context) {
	variant := c.Param("variant")
	if strings.TrimSpace(variant) == "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"MISSING_PARAMETER",
			"Variant parameter is required",
			nil,
		))
		return
	}

	var kind *string
	if kindParam := strings.TrimSpace(c.Query("kind")); kindParam != "" {
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

	words, err := h.service.GetWordsByVariant(variant, kind, includePronunciations, includeSenses)
	if err != nil {
		if errors.Is(err, repository.ErrVariantNotFound) {
			c.JSON(http.StatusNotFound, model.NewErrorResponse(
				"WORD_NOT_FOUND",
				"Variant '"+variant+"' not found in dictionary",
				nil,
			))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
			"INTERNAL_ERROR",
			err.Error(),
			nil,
		))
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
			"Invalid request body: "+err.Error(),
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

	// Check batch size limit early to avoid wasting resources on oversized requests
	if len(req.Words) > h.config.APIBatchMaxSize {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"BATCH_LIMIT_EXCEEDED",
			"Batch size exceeds the maximum limit of "+strconv.Itoa(h.config.APIBatchMaxSize)+" words",
			nil,
		))
		return
	}

	cleanedWords := make([]string, 0, len(req.Words))
	seen := make(map[string]struct{}, len(req.Words))
	for _, w := range req.Words {
		trimmed := strings.TrimSpace(w)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		cleanedWords = append(cleanedWords, trimmed)
	}

	if len(cleanedWords) == 0 {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			"No valid words provided",
			nil,
		))
		return
	}

	req.Words = cleanedWords

	words, meta, err := h.service.GetWordsBatch(&req)
	if err != nil {
		if errors.Is(err, service.ErrBatchLimitExceeded) {
			c.JSON(http.StatusBadRequest, model.NewErrorResponse(
				"BATCH_LIMIT_EXCEEDED",
				err.Error(),
				nil,
			))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
			"INTERNAL_ERROR",
			err.Error(),
			nil,
		))
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponseWithMeta(words, meta))
}

// SearchWords handles GET /api/v1/search
func (h *WordHandler) SearchWords(c *gin.Context) {
	keyword := c.Query("q")
	if strings.TrimSpace(keyword) == "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"MISSING_PARAMETER",
			"Query parameter 'q' is required",
			nil,
		))
		return
	}

	// Validate minimum query length
	if err := validateQueryLength(keyword); err != nil {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			err.Error(),
			map[string]interface{}{
				"min_length": MinQueryLength,
				"provided":   utf8.RuneCountInString(strings.TrimSpace(keyword)),
			},
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

	results, meta, err := h.service.SearchWords(keyword, posCode, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
			"INTERNAL_ERROR",
			err.Error(),
			nil,
		))
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponseWithMeta(results, meta))
}

// SuggestWords handles GET /api/v1/suggest
func (h *WordHandler) SuggestWords(c *gin.Context) {
	prefix := c.Query("prefix")
	if strings.TrimSpace(prefix) == "" {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"MISSING_PARAMETER",
			"Query parameter 'prefix' is required",
			nil,
		))
		return
	}

	// Validate minimum query length
	if err := validateQueryLength(prefix); err != nil {
		c.JSON(http.StatusBadRequest, model.NewErrorResponse(
			"INVALID_PARAMETER",
			err.Error(),
			map[string]interface{}{
				"min_length": MinQueryLength,
				"provided":   utf8.RuneCountInString(strings.TrimSpace(prefix)),
			},
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

	results, err := h.service.SuggestWords(prefix, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
			"INTERNAL_ERROR",
			err.Error(),
			nil,
		))
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

	results, err := h.service.SearchPhrases(keyword, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
			"INTERNAL_ERROR",
			err.Error(),
			nil,
		))
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

	pronunciations, err := h.service.GetPronunciations(headword, accentCode)
	if err != nil {
		if errors.Is(err, repository.ErrWordNotFound) {
			c.JSON(http.StatusNotFound, model.NewErrorResponse(
				"WORD_NOT_FOUND",
				"Word '"+headword+"' not found in dictionary",
				nil,
			))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
			"INTERNAL_ERROR",
			err.Error(),
			nil,
		))
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

	lang := strings.ToLower(c.DefaultQuery("lang", "both"))
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

	senses, err := h.service.GetSenses(headword, posCode, lang)
	if err != nil {
		if errors.Is(err, repository.ErrWordNotFound) {
			c.JSON(http.StatusNotFound, model.NewErrorResponse(
				"WORD_NOT_FOUND",
				"Word '"+headword+"' not found in dictionary",
				nil,
			))
			return
		}
		c.JSON(http.StatusInternalServerError, model.NewErrorResponse(
			"INTERNAL_ERROR",
			err.Error(),
			nil,
		))
		return
	}

	c.JSON(http.StatusOK, model.NewSuccessResponse(senses))
}
