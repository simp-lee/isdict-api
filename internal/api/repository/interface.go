package repository

import "github.com/simp-lee/isdict-commons/model"

// WordRepository defines the interface for word data access
type WordRepository interface {
	GetWordByHeadword(headword string, includeVariants, includePronunciations, includeSenses bool) (*model.Word, *model.WordVariant, error)
	GetWordsByHeadwords(headwords []string, includeVariants, includePronunciations, includeSenses bool) ([]model.Word, error)
	GetWordsByVariant(variant string, kind *int) ([]model.Word, []model.WordVariant, error)
	SearchWords(keyword string, pos *int, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit, offset int) ([]model.Word, int64, error)
	SuggestWords(prefix string, cefrLevel *int, oxfordLevel *int, cetLevel *int, maxFrequencyRank *int, minCollinsStars *int, limit int) ([]model.Word, error)
	SearchPhrases(keyword string, limit int) ([]model.Word, error)
	GetPronunciationsByWordID(wordID uint, accent *int) ([]model.Pronunciation, error)
	GetSensesByWordID(wordID uint, pos *int) ([]model.Sense, error)
}

// Ensure Repository implements WordRepository interface
var _ WordRepository = (*Repository)(nil)
