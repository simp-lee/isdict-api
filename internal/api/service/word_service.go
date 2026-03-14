package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/simp-lee/isdict-api/internal/api/contracts"
	"github.com/simp-lee/isdict-api/internal/api/queryvalidation"
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
	ErrWordNotFound       = contracts.ErrWordNotFound
	ErrVariantNotFound    = contracts.ErrVariantNotFound
)

const (
	langBoth    = "both"
	langEnglish = "en"
	langChinese = "zh"
)

type batchCandidate struct {
	word    *model.Word
	variant *model.WordVariant
}

type batchCandidateIndex map[string][]batchCandidate

type batchIncludeOptions struct {
	variants       bool
	pronunciations bool
	senses         bool
}

// NewWordService creates a new word service instance
func NewWordService(repo repository.WordRepository, cfg *config.Config) *WordService {
	return &WordService{
		repo:   repo,
		config: cfg,
	}
}

// GetWordByHeadword retrieves a word by headword
func (s *WordService) GetWordByHeadword(ctx context.Context, headword string, accentCode *int, includeVariants, includePronunciations, includeSenses bool) (*model.WordResponse, error) {
	word, variant, err := s.repo.GetWordByHeadword(ctx, headword, includeVariants, includePronunciations, includeSenses)
	if err != nil {
		return nil, translateWordNotFound(err)
	}

	return s.convertToWordResponse(word, variant, accentCode, includeVariants, includePronunciations, includeSenses), nil
}

// GetWordsByVariant finds words by variant text
func (s *WordService) GetWordsByVariant(ctx context.Context, variant string, kindStr *string, includePronunciations, includeSenses bool) ([]model.VariantReverseResponse, error) {
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

	words, variants, err := s.repo.GetWordsByVariant(ctx, variant, kind, includePronunciations, includeSenses)
	if err != nil {
		return nil, translateVariantNotFound(err)
	}

	// Create a map for quick variant lookup - support multiple variants per word
	variantMap := make(map[uint][]model.WordVariant)
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
				CEFRLevel:      toAPICEFRLevel(word.CEFRLevel),
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
func (s *WordService) GetWordsBatch(ctx context.Context, req *model.BatchRequest) ([]model.WordResponse, *model.MetaInfo, error) {
	includeOptions, err := s.prepareBatchRequest(req)
	if err != nil {
		return nil, nil, err
	}
	if len(req.Words) == 0 {
		return []model.WordResponse{}, nil, nil
	}

	words, err := s.repo.GetWordsByHeadwords(ctx, req.Words, includeOptions.variants, includeOptions.pronunciations, includeOptions.senses)
	if err != nil {
		return nil, nil, err
	}
	index := buildBatchCandidateIndex(words)
	if err := s.fillBatchVariantFallback(ctx, req.Words, index, includeOptions); err != nil {
		return nil, nil, err
	}

	responses, notFound := s.buildBatchResponses(req.Words, index, includeOptions)
	requested := len(req.Words)
	found := len(responses)

	meta := &model.MetaInfo{
		Requested: &requested,
		Found:     &found,
		NotFound:  notFound,
	}

	return responses, meta, nil
}

func (s *WordService) prepareBatchRequest(req *model.BatchRequest) (batchIncludeOptions, error) {
	includeOptions := resolveBatchIncludeOptions(req)
	if len(req.Words) == 0 {
		return includeOptions, nil
	}

	cleanedWords := queryvalidation.NormalizeBatchWords(req.Words)
	if len(cleanedWords) == 0 {
		req.Words = cleanedWords
		return includeOptions, nil
	}
	if len(cleanedWords) > s.config.APIBatchMaxSize {
		return batchIncludeOptions{}, fmt.Errorf("%w: maximum %d words per request", ErrBatchLimitExceeded, s.config.APIBatchMaxSize)
	}
	req.Words = cleanedWords
	return includeOptions, nil
}

func resolveBatchIncludeOptions(req *model.BatchRequest) batchIncludeOptions {
	options := batchIncludeOptions{
		variants:       true,
		pronunciations: true,
		senses:         true,
	}
	if req == nil {
		return options
	}
	if req.IncludeVariants != nil {
		options.variants = *req.IncludeVariants
	}
	if req.IncludePronunciations != nil {
		options.pronunciations = *req.IncludePronunciations
	}
	if req.IncludeSenses != nil {
		options.senses = *req.IncludeSenses
	}
	return options
}

func buildBatchCandidateIndex(words []repository.Word) batchCandidateIndex {
	index := make(batchCandidateIndex, len(words))
	for i := range words {
		index.addCandidate(&words[i], nil, "")
	}
	return index
}

func (index batchCandidateIndex) addCandidate(word *model.Word, variant *model.WordVariant, alias string) {
	if word == nil {
		return
	}
	for _, key := range batchCandidateKeys(word, alias) {
		if index.hasCandidate(key, word, variant) {
			continue
		}
		index[key] = append(index[key], batchCandidate{word: word, variant: variant})
	}
}

func batchCandidateKeys(word *model.Word, alias string) []string {
	keys := []string{textutil.ToNormalized(word.Headword)}
	if trimmed := strings.TrimSpace(alias); trimmed != "" {
		aliasKey := textutil.ToNormalized(trimmed)
		if aliasKey != "" && aliasKey != keys[0] {
			keys = append(keys, aliasKey)
		}
	}
	return keys
}

func (index batchCandidateIndex) hasCandidate(key string, word *model.Word, variant *model.WordVariant) bool {
	for _, existing := range index[key] {
		if sameBatchCandidate(existing, batchCandidate{word: word, variant: variant}) {
			return true
		}
	}
	return false
}

func sameBatchCandidate(left, right batchCandidate) bool {
	if left.word == nil || right.word == nil || left.word.ID != right.word.ID {
		return false
	}
	if left.variant == nil || right.variant == nil {
		return left.variant == nil && right.variant == nil
	}
	return left.variant.VariantText == right.variant.VariantText
}

func (index batchCandidateIndex) selectCandidate(input string) (batchCandidate, bool) {
	key := textutil.ToNormalized(input)
	candidates := index[key]
	if len(candidates) == 0 {
		return batchCandidate{}, false
	}
	return preferredBatchCandidate(candidates, input), true
}

func preferredBatchCandidate(candidates []batchCandidate, input string) batchCandidate {
	for _, candidate := range candidates {
		if candidate.word.Headword == input {
			return candidate
		}
	}
	for _, candidate := range candidates {
		if candidate.variant != nil && candidate.variant.VariantText == input {
			return candidate
		}
	}
	for _, candidate := range candidates {
		if candidate.word.Headword == strings.ToLower(candidate.word.Headword) {
			return candidate
		}
	}
	return candidates[0]
}

func unresolvedBatchWords(words []string, index batchCandidateIndex) []string {
	missing := make([]string, 0)
	for _, word := range words {
		if _, ok := index.selectCandidate(word); !ok {
			missing = append(missing, word)
		}
	}
	return missing
}

func (s *WordService) fillBatchVariantFallback(ctx context.Context, inputs []string, index batchCandidateIndex, options batchIncludeOptions) error {
	missing := unresolvedBatchWords(inputs, index)
	if len(missing) == 0 {
		return nil
	}

	matches, err := s.repo.GetWordsByVariants(ctx, missing, options.variants, options.pronunciations, options.senses)
	if err != nil {
		return err
	}
	for i := range matches {
		index.addCandidate(&matches[i].Word, &matches[i].Variant, matches[i].Variant.VariantText)
	}
	return nil
}

func (s *WordService) buildBatchResponses(inputs []string, index batchCandidateIndex, options batchIncludeOptions) ([]model.WordResponse, []string) {
	responses := make([]model.WordResponse, 0, len(inputs))
	notFound := make([]string, 0)
	for _, input := range inputs {
		candidate, ok := index.selectCandidate(input)
		if !ok {
			notFound = append(notFound, input)
			continue
		}
		responses = append(responses, *s.convertToWordResponse(candidate.word, candidate.variant, nil, options.variants, options.pronunciations, options.senses))
	}
	return responses, notFound
}

// SearchWords performs fuzzy search
func (s *WordService) SearchWords(ctx context.Context, keyword string, posCode *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.SearchResultResponse, *model.MetaInfo, error) {
	// Validate keyword length
	keywordLength := queryvalidation.NormalizedRuneCount(keyword)
	if keywordLength < queryvalidation.MinQueryLength {
		return nil, nil, fmt.Errorf("keyword must be at least %d characters", queryvalidation.MinQueryLength)
	}
	if queryvalidation.TrimmedRuneCount(keyword) > 100 {
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

	words, total, err := s.repo.SearchWords(ctx, keyword, posCode, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit, offset)
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
				CEFRLevel:      toAPICEFRLevel(word.CEFRLevel),
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

	meta := &model.MetaInfo{
		Total:  &total,
		Limit:  &limit,
		Offset: &offset,
	}

	return results, meta, nil
}

// SuggestWords provides autocomplete suggestions
func (s *WordService) SuggestWords(ctx context.Context, prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.SuggestResponse, error) {
	// Validate prefix length
	prefixLength := queryvalidation.NormalizedRuneCount(prefix)
	if prefixLength < queryvalidation.MinQueryLength {
		return nil, fmt.Errorf("prefix must be at least %d characters", queryvalidation.MinQueryLength)
	}
	if queryvalidation.TrimmedRuneCount(prefix) > 50 {
		return nil, errors.New("prefix must not exceed 50 characters")
	}

	if limit <= 0 {
		limit = 10
	}
	if limit > s.config.APISuggestMaxLimit {
		limit = s.config.APISuggestMaxLimit
	}

	words, err := s.repo.SuggestWords(ctx, prefix, cefrLevel, oxfordLevel, cetLevel, maxFrequencyRank, minCollinsStars, limit)
	if err != nil {
		return nil, err
	}

	results := make([]model.SuggestResponse, 0, len(words))
	for _, word := range words {
		results = append(results, model.SuggestResponse{
			Headword: word.Headword,
			WordAnnotations: model.WordAnnotations{
				CEFRLevel:      toAPICEFRLevel(word.CEFRLevel),
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
func (s *WordService) SearchPhrases(ctx context.Context, keyword string, limit int) ([]model.SuggestResponse, error) {
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

	words, err := s.repo.SearchPhrases(ctx, keyword, limit)
	if err != nil {
		return nil, err
	}

	results := make([]model.SuggestResponse, 0, len(words))
	for _, word := range words {
		results = append(results, model.SuggestResponse{
			Headword: word.Headword,
			WordAnnotations: model.WordAnnotations{
				CEFRLevel:      toAPICEFRLevel(word.CEFRLevel),
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
func (s *WordService) GetPronunciations(ctx context.Context, headword string, accentCode *int) ([]model.PronunciationResponse, error) {
	word, _, err := s.repo.GetWordByHeadword(ctx, headword, false, false, false)
	if err != nil {
		return nil, translateWordNotFound(err)
	}

	pronunciations, err := s.repo.GetPronunciationsByWordID(ctx, word.ID, accentCode)
	if err != nil {
		return nil, err
	}

	return s.convertPronunciations(pronunciations, accentCode), nil
}

// GetSenses retrieves senses for a word
func (s *WordService) GetSenses(ctx context.Context, headword string, posCode *int, lang string) ([]model.SenseResponse, error) {
	word, _, err := s.repo.GetWordByHeadword(ctx, headword, false, false, false)
	if err != nil {
		return nil, translateWordNotFound(err)
	}

	senses, err := s.repo.GetSensesByWordID(ctx, word.ID, posCode)
	if err != nil {
		return nil, err
	}

	return s.convertSenses(senses, lang), nil
}

// Helper methods for conversion

func (s *WordService) convertToWordResponse(word *model.Word, variant *model.WordVariant, accentCode *int, includeVariants, includePronunciations, includeSenses bool) *model.WordResponse {
	resp := &model.WordResponse{
		ID:       word.ID,
		Headword: word.Headword,
		WordAnnotations: model.WordAnnotations{
			CEFRLevel:      toAPICEFRLevel(word.CEFRLevel),
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
	if variant != nil {
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

func (s *WordService) convertPronunciations(pronunciations []model.Pronunciation, accentCode *int) []model.PronunciationResponse {
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

func (s *WordService) convertSenses(senses []model.Sense, lang string) []model.SenseResponse {
	results := make([]model.SenseResponse, 0, len(senses))
	for _, sense := range senses {
		senseResp := model.SenseResponse{
			SenseID:      sense.ID,
			POS:          model.GetPOSName(sense.POS),
			CEFRLevel:    toAPICEFRLevel(sense.CEFRLevel),
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

func (s *WordService) convertExamples(examples []model.Example, lang string) []model.ExampleResponse {
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

func (s *WordService) convertVariants(variants []model.WordVariant) []model.VariantResponse {
	results := make([]model.VariantResponse, 0, len(variants))
	for _, v := range variants {
		results = append(results, *s.convertVariant(v))
	}
	return results
}

func (s *WordService) convertVariant(v model.WordVariant) *model.VariantResponse {
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

func toAPICEFRLevel(dbLevel int) string {
	return model.GetCEFRLevelName(dbLevel)
}

func translateWordNotFound(err error) error {
	if errors.Is(err, repository.ErrWordNotFound) {
		return contracts.ErrWordNotFound
	}

	return err
}

func translateVariantNotFound(err error) error {
	if errors.Is(err, repository.ErrVariantNotFound) {
		return contracts.ErrVariantNotFound
	}

	return err
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
