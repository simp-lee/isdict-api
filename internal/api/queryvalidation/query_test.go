package queryvalidation

import (
	"reflect"
	"testing"
)

func TestNormalizeBatchWords_DeduplicatesOnlyTrimmedExactMatches(t *testing.T) {
	words := []string{"cooperate", " cooperate ", "it's", " it's ", "   "}
	got := NormalizeBatchWords(words)
	want := []string{"cooperate", "it's"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeBatchWords() = %v, want %v", got, want)
	}
}

func TestNormalizeBatchWords_PreservesCaseDistinctInputs(t *testing.T) {
	words := []string{"Polish", "polish"}
	got := NormalizeBatchWords(words)

	if !reflect.DeepEqual(got, words) {
		t.Fatalf("NormalizeBatchWords() = %v, want %v", got, words)
	}
}

func TestNormalizeBatchWords_PreservesSeparatorDistinctInputs(t *testing.T) {
	words := []string{"re-sign", "resign", "ice cream", "icecream", "snake_case", "snakecase"}
	got := NormalizeBatchWords(words)

	if !reflect.DeepEqual(got, words) {
		t.Fatalf("NormalizeBatchWords() = %v, want %v", got, words)
	}
}
