package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/simp-lee/isdict-api/internal/api/repository"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/model"
	"github.com/simp-lee/isdict-commons/textutil"
)

// WordService handles business logic for word operations
type WordService struct {
	repo   repository.WordRepository
	config *config.Config
}

// Domain-level errors produced by the service layer
var (
	ErrBatchLimitExceeded = errors.New("batch limit exceeded")
)

const (
	langBoth    = "both"
	langEnglish = "en"
	langChinese = "zh"
)

// NewWordService creates a new word service instance
func NewWordService(repo repository.WordRepository, cfg *config.Config) *WordService {
	return &WordService{
		repo:   repo,
		config: cfg,
	}
}

// GetWordByHeadword retrieves a word by headword
func (s *WordService) GetWordByHeadword(headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error) {
	word, variant, err := s.repo.GetWordByHeadword(headword, includeVariants, includePronunciations, includeSenses)
	if err != nil {
		return nil, err
	}

	return s.convertToWordResponse(word, variant, accentCode, includeVariants, includePronunciations, includeSenses), nil
}

// GetWordsByVariant finds words by variant text
func (s *WordService) GetWordsByVariant(variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error) {
	var kind *int
	if kindStr != nil {
		// kindStr is already lowercase and validated by the handler layer
		switch *kindStr {
		case "form":
			k := int(model.VariantForm)
			kind = &k
		case "alias":
			k := int(model.VariantAlias)
			kind = &k
		}
	}

	words, variants, err := s.repo.GetWordsByVariant(variant, kind)
	if err != nil {
		if errors.Is(err, repository.ErrVariantNotFound) {
			return nil, repository.ErrVariantNotFound
		}
		return nil, err
	}

	// Create a map for quick variant lookup - support multiple variants per word
	variantMap := make(map[uint][]repository.WordVariant)
	for i := range variants {
		wordID := variants[i].WordID
		variantMap[wordID] = append(variantMap[wordID], variants[i])
	}

	results := make([]model.VariantReverseResponse, 0, len(words))
	for _, word := range words {
		resp := model.VariantReverseResponse{
			ID:       word.ID,
			Headword: word.Headword,
			WordAnnotations: model.WordAnnotations{
				CEFRLevel:      word.CEFRLevel,
				CEFRSource:     word.CEFRSource,
				CETLevel:       toAPICETLevel(word.CETLevel),
				OxfordLevel:    word.OxfordLevel,
				SchoolLevel:    word.SchoolLevel,
				FrequencyRank:  word.FrequencyRank,
				FrequencyCount: word.FrequencyCount,
				CollinsStars:   word.CollinsStars,
				TranslationZH:  word.TranslationZH,
			},
		}

		if includePronunciations {
			resp.Pronunciations = s.convertPronunciations(word.Pronunciations, nil)
		}

		if includeSenses {
			resp.Senses = s.convertSenses(word.Senses, langBoth)
		}

		// Add all matched variant info for this word
		if variants, ok := variantMap[word.ID]; ok {
			resp.VariantInfo = make([]model.VariantResponse, 0, len(variants))
			for _, v := range variants {
				resp.VariantInfo = append(resp.VariantInfo, *s.convertVariant(v))
			}
		}

		results = append(results, resp)
	}

	return results, nil
}

// GetWordsBatch retrieves multiple words with automatic fallback to variants
func (s *WordService) GetWordsBatch(req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error) {
	if len(req.Words) == 0 {
		return []model.WordResponse{}, nil, nil
	}

	if len(req.Words) > s.config.APIBatchMaxSize {
		return nil, nil, fmt.Errorf("%w: maximum %d words per request", ErrBatchLimitExceeded, s.config.APIBatchMaxSize)
	}

	// Set defaults
	includeVariants := true
	includePronunciations := true
	includeSenses := true

	if req.IncludeVariants != nil {
		includeVariants = *req.IncludeVariants
	}
	if req.IncludePronunciations != nil {
		includePronunciations = *req.IncludePronunciations
	}
	if req.IncludeSenses != nil {
		includeSenses = *req.IncludeSenses
	}

	// ============ Step 1: Batch query main words table ============
	words, err := s.repo.GetWordsByHeadwords(req.Words, includeVariants, includePronunciations, includeSenses)
	if err != nil {
		return nil, nil, err
	}

	// ============ Step 2: Build index using normalized form ============
	wordGroups := make(map[string][]*repository.Word, len(words))
	addCandidate := func(word *repository.Word, alias string) {
		if word == nil {
			return
		}

		keys := []string{textutil.ToNormalized(word.Headword)}
		if trimmed := strings.TrimSpace(alias); trimmed != "" {
			aliasKey := textutil.ToNormalized(trimmed)
			if aliasKey != "" && aliasKey != keys[0] {
				keys = append(keys, aliasKey)
			}
		}

		for _, key := range keys {
			group := wordGroups[key]
			duplicate := false
			for _, existing := range group {
				if existing.ID == word.ID {
					duplicate = true
					break
				}
			}
			if !duplicate {
				wordGroups[key] = append(group, word)
			}
		}
	}

	for i := range words {
		addCandidate(&words[i], "")
	}

	selectWord := func(input string) (*repository.Word, bool) {
		key := textutil.ToNormalized(input)
		candidates, ok := wordGroups[key]
		if !ok || len(candidates) == 0 {
			return nil, false
		}
		for _, candidate := range candidates {
			if candidate.Headword == input {
				return candidate, true
			}
		}
		for _, candidate := range candidates {
			if candidate.Headword == strings.ToLower(candidate.Headword) {
				return candidate, true
			}
		}
		return candidates[0], true
	}

	// ============ Step 3: Find words not found in main table ============
	notFoundHeadwords := make([]string, 0)
	for _, hw := range req.Words {
		if _, found := selectWord(hw); !found {
			notFoundHeadwords = append(notFoundHeadwords, hw)
		}
	}

	// ============ Step 4: Query variants for not found words (fallback) ============
	for _, hw := range notFoundHeadwords {
		word, _, err := s.repo.GetWordByHeadword(hw, includeVariants, includePronunciations, includeSenses)
		if err != nil {
			if errors.Is(err, repository.ErrWordNotFound) {
				continue
			}
			return nil, nil, err
		}
		addCandidate(word, hw)
	}

	// ============ Step 5: Build response in request order ============
	responses := make([]model.WordResponse, 0, len(req.Words))
	notFound := make([]string, 0)
	for _, hw := range req.Words {
		if word, ok := selectWord(hw); ok {
			responses = append(responses, *s.convertToWordResponse(word, nil, nil, includeVariants, includePronunciations, includeSenses))
		} else {
			notFound = append(notFound, hw)
		}
	}

	// ============ Step 6: Return results and metadata ============
	requested := len(req.Words)
	found := len(responses)

	meta := &model.MetaInfo{
		Requested: &requested,
		Found:     &found,
		NotFound:  notFound,
	}

	return responses, meta, nil
}

// SearchWords performs fuzzy search
func (s *WordService) SearchWords(keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
	// Validate keyword length
	keywordRunes := []rune(keyword)
	if len(keywordRunes) < 2 {
		return nil, nil, errors.New("keyword must be at least 2 characters")
	}
	if len(keywordRunes) > 100 {
		return nil, nil, errors.New("keyword must not exceed 100 characters")
	}

	// Validate and set defaults
	if limit <= 0 {
		limit = 20
	}
	if limit > s.config.APISearchMaxLimit {
		limit = s.config.APISearchMaxLimit
	}
	if offset < 0 {
		offset = 0
	}

	words, total, err := s.repo.SearchWords(keyword, posCode, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit, offset)
	if err != nil {
		return nil, nil, err
	}

	results := make([]model.SearchResultResponse, 0, len(words))
	for _, word := range words {
		// Get distinct POS values
		posNames := make([]string, 0, len(word.Senses))
		posSet := make(map[string]bool)
		for _, sense := range word.Senses {
			posName := model.GetPOSName(sense.POS)
			if !posSet[posName] {
				posSet[posName] = true
				posNames = append(posNames, posName)
			}
		}

		results = append(results, model.SearchResultResponse{
			ID:       word.ID,
			Headword: word.Headword,
			POS:      posNames,
			WordAnnotations: model.WordAnnotations{
				CEFRLevel:      word.CEFRLevel,
				CETLevel:       toAPICETLevel(word.CETLevel),
				OxfordLevel:    word.OxfordLevel,
				SchoolLevel:    word.SchoolLevel,
				FrequencyRank:  word.FrequencyRank,
				FrequencyCount: word.FrequencyCount,
				CollinsStars:   word.CollinsStars,
				TranslationZH:  word.TranslationZH,
			},
		})
	}

	meta := &model.MetaInfo{
		Total:  &total,
		Limit:  &limit,
		Offset: &offset,
	}

	return results, meta, nil
}

// SuggestWords provides autocomplete suggestions
func (s *WordService) SuggestWords(prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
	// Validate prefix length
	prefixRunes := []rune(prefix)
	if len(prefixRunes) < 1 {
		return nil, errors.New("prefix must be at least 1 character")
	}
	if len(prefixRunes) > 50 {
		return nil, errors.New("prefix must not exceed 50 characters")
	}

	if limit <= 0 {
		limit = 10
	}
	if limit > s.config.APISuggestMaxLimit {
		limit = s.config.APISuggestMaxLimit
	}

	words, err := s.repo.SuggestWords(prefix, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit)
	if err != nil {
		return nil, err
	}

	results := make([]model.SuggestResponse, 0, len(words))
	for _, word := range words {
		results = append(results, model.SuggestResponse{
			Headword: word.Headword,
			WordAnnotations: model.WordAnnotations{
				CEFRLevel:      word.CEFRLevel,
				CEFRSource:     word.CEFRSource,
				CETLevel:       toAPICETLevel(word.CETLevel),
				OxfordLevel:    word.OxfordLevel,
				SchoolLevel:    word.SchoolLevel,
				FrequencyRank:  word.FrequencyRank,
				FrequencyCount: word.FrequencyCount,
				CollinsStars:   word.CollinsStars,
				TranslationZH:  word.TranslationZH,
			},
		})
	}

	return results, nil
}

// SearchPhrases searches for phrases containing the keyword
func (s *WordService) SearchPhrases(keyword string, limit int) ([]model.SuggestResponse, error) {
	// Validate keyword
	keywordRunes := []rune(strings.TrimSpace(keyword))
	if len(keywordRunes) < 1 {
		return nil, errors.New("keyword must be at least 1 character")
	}
	if len(keywordRunes) > 50 {
		return nil, errors.New("keyword must not exceed 50 characters")
	}

	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	words, err := s.repo.SearchPhrases(keyword, limit)
	if err != nil {
		return nil, err
	}

	results := make([]model.SuggestResponse, 0, len(words))
	for _, word := range words {
		results = append(results, model.SuggestResponse{
			Headword: word.Headword,
			WordAnnotations: model.WordAnnotations{
				CEFRLevel:      word.CEFRLevel,
				CEFRSource:     word.CEFRSource,
				CETLevel:       toAPICETLevel(word.CETLevel),
				OxfordLevel:    word.OxfordLevel,
				SchoolLevel:    word.SchoolLevel,
				FrequencyRank:  word.FrequencyRank,
				FrequencyCount: word.FrequencyCount,
				CollinsStars:   word.CollinsStars,
				TranslationZH:  word.TranslationZH,
			},
		})
	}

	return results, nil
}

// GetPronunciations retrieves pronunciations for a word
func (s *WordService) GetPronunciations(headword string, accentCode *int) ([]model.PronunciationResponse, error) {
	word, _, err := s.repo.GetWordByHeadword(headword, false, false, false)
	if err != nil {
		return nil, err
	}

	pronunciations, err := s.repo.GetPronunciationsByWordID(word.ID, accentCode)
	if err != nil {
		return nil, err
	}

	return s.convertPronunciations(pronunciations, accentCode), nil
}

// GetSenses retrieves senses for a word
func (s *WordService) GetSenses(headword string, posCode *int, lang string) ([]model.SenseResponse, error) {
	word, _, err := s.repo.GetWordByHeadword(headword, false, false, false)
	if err != nil {
		return nil, err
	}

	senses, err := s.repo.GetSensesByWordID(word.ID, posCode)
	if err != nil {
		return nil, err
	}

	return s.convertSenses(senses, lang), nil
}

// Helper methods for conversion

func (s *WordService) convertToWordResponse(word *repository.Word, variant *repository.WordVariant, accentCode *int, includeVariants, includePronunciations, includeSenses bool) *model.WordResponse {
	resp := &model.WordResponse{
		ID:       word.ID,
		Headword: word.Headword,
		WordAnnotations: model.WordAnnotations{
			CEFRLevel:      word.CEFRLevel,
			CEFRSource:     word.CEFRSource,
			CETLevel:       toAPICETLevel(word.CETLevel),
			OxfordLevel:    word.OxfordLevel,
			SchoolLevel:    word.SchoolLevel,
			FrequencyRank:  word.FrequencyRank,
			FrequencyCount: word.FrequencyCount,
			CollinsStars:   word.CollinsStars,
			TranslationZH:  word.TranslationZH,
		},
	}

	// If word was found via variant, add queried variant info
	if variant != nil && variant.FrequencyRank > 0 {
		usageRatio := 0.0
		if word.FrequencyCount > 0 {
			usageRatio = float64(variant.FrequencyCount) / float64(word.FrequencyCount) * 100
		}
		resp.QueriedVariant = &model.QueriedVariantInfo{
			Text:           variant.VariantText,
			FrequencyRank:  variant.FrequencyRank,
			FrequencyCount: variant.FrequencyCount,
			UsageRatio:     usageRatio,
		}
	}

	if includePronunciations {
		resp.Pronunciations = s.convertPronunciations(word.Pronunciations, accentCode)
	}

	if includeSenses {
		resp.Senses = s.convertSenses(word.Senses, langBoth)
	}

	if includeVariants {
		resp.Variants = s.convertVariants(word.WordVariants)
	}

	return resp
}

func (s *WordService) convertPronunciations(pronunciations []repository.Pronunciation, accentCode *int) []model.PronunciationResponse {
	results := make([]model.PronunciationResponse, 0, len(pronunciations))
	for _, p := range pronunciations {
		if accentCode != nil && p.Accent != *accentCode {
			continue
		}
		accentName := model.GetAccentName(p.Accent)
		results = append(results, model.PronunciationResponse{
			Accent:    accentName,
			IPA:       p.IPA,
			IsPrimary: p.IsPrimary,
		})
	}
	return results
}

func (s *WordService) convertSenses(senses []repository.Sense, lang string) []model.SenseResponse {
	results := make([]model.SenseResponse, 0, len(senses))
	for _, sense := range senses {
		senseResp := model.SenseResponse{
			SenseID:      sense.ID,
			POS:          model.GetPOSName(sense.POS),
			CEFRLevel:    sense.CEFRLevel,
			CEFRSource:   sense.CEFRSource,
			OxfordLevel:  sense.OxfordLevel,
			DefinitionEN: sense.DefinitionEN,
			DefinitionZH: sense.DefinitionZH,
			SenseOrder:   sense.SenseOrder,
			Examples:     s.convertExamples(sense.Examples, lang),
		}

		applyDefinitionLangFilter(&senseResp, lang)
		results = append(results, senseResp)
	}
	return results
}

func (s *WordService) convertExamples(examples []repository.Example, lang string) []model.ExampleResponse {
	results := make([]model.ExampleResponse, 0, len(examples))
	for _, ex := range examples {
		exampleResp := model.ExampleResponse{
			ExampleID:    ex.ID,
			SentenceEN:   ex.SentenceEN,
			SentenceZH:   ex.SentenceZH,
			ExampleOrder: ex.ExampleOrder,
		}

		applyExampleLangFilter(&exampleResp, lang)
		results = append(results, exampleResp)
	}
	return results
}

// applyDefinitionLangFilter removes unwanted language fields from sense definitions based on the lang parameter
func applyDefinitionLangFilter(senseResp *model.SenseResponse, lang string) {
	switch lang {
	case langEnglish:
		senseResp.DefinitionZH = ""
	case langChinese:
		senseResp.DefinitionEN = ""
	}
}

// applyExampleLangFilter removes unwanted language fields from examples based on the lang parameter
func applyExampleLangFilter(exampleResp *model.ExampleResponse, lang string) {
	switch lang {
	case langEnglish:
		exampleResp.SentenceZH = ""
	case langChinese:
		exampleResp.SentenceEN = ""
	}
}

func (s *WordService) convertVariants(variants []repository.WordVariant) []model.VariantResponse {
	results := make([]model.VariantResponse, 0, len(variants))
	for _, v := range variants {
		results = append(results, *s.convertVariant(v))
	}
	return results
}

func (s *WordService) convertVariant(v repository.WordVariant) *model.VariantResponse {
	resp := &model.VariantResponse{
		VariantText:    v.VariantText,
		Kind:           model.GetVariantKindName(int(v.Kind)),
		Tags:           v.Tags,
		FrequencyRank:  v.FrequencyRank,
		FrequencyCount: v.FrequencyCount,
	}
	if v.FormType != nil {
		resp.FormType = model.GetFormTypeName(*v.FormType)
	}
	return resp
}

// toAPICETLevel converts the database CET level encoding (0,1,2) to the
// external API contract (0,4,6). The database keeps compact enum values to
// simplify indexing, while API consumers expect the canonical CET identifiers.
func toAPICETLevel(dbLevel int) int {
	switch dbLevel {
	case 1:
		return 4
	case 2:
		return 6
	default:
		return 0
	}
}
