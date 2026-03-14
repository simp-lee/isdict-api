package queryvalidation

import (
	"strings"
	"unicode/utf8"

	"github.com/simp-lee/isdict-commons/textutil"
)

const MinQueryLength = 3

func TrimmedRuneCount(query string) int {
	return utf8.RuneCountInString(strings.TrimSpace(query))
}

func NormalizedRuneCount(query string) int {
	return utf8.RuneCountInString(textutil.ToNormalized(query))
}

func NormalizeBatchWords(words []string) []string {
	cleanedWords := make([]string, 0, len(words))
	seen := make(map[string]struct{}, len(words))
	for _, word := range words {
		trimmed := strings.TrimSpace(word)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		cleanedWords = append(cleanedWords, trimmed)
	}

	return cleanedWords
}
