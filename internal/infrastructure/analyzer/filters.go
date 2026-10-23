package analyzer

import (
	"strings"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

// Filter defines a transformation step in the analyzer pipeline.
type Filter interface {
	Apply(tokens []domain.Token) []domain.Token
}

// LowercaseFilter converts token values to lowercase and normalizes Cyrillic 'ё' to 'е'.
type LowercaseFilter struct{}

func (f LowercaseFilter) Apply(tokens []domain.Token) []domain.Token {
	for i := range tokens {
		val := strings.ToLower(tokens[i].Value)
		if strings.ContainsRune(val, 'ё') {
			val = strings.ReplaceAll(val, "ё", "е")
		}
		tokens[i].Value = val
	}
	return tokens
}
