package analyzer

import (
	"strings"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

type Filter interface {
	Apply(tokens []domain.Token) []domain.Token
}

type LowercaseFilter struct{}

func (f LowercaseFilter) Apply(tokens []domain.Token) []domain.Token {
	for i := range tokens {
		tokens[i].Value = strings.ToLower(tokens[i].Value)
	}
	return tokens
}
