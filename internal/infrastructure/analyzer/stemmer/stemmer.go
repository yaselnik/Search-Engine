package stemmer

import (
	"unicode"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

type Stemmer interface {
	Stem(word string) string
}

type MultiLanguageStemmer struct {
	Eng Stemmer
	Ru  Stemmer
}

func NewMultiLanguageStemmer(eng, ru Stemmer) *MultiLanguageStemmer{
	return &MultiLanguageStemmer{
		Eng: eng,
		Ru: ru,
	}
}

func (s *MultiLanguageStemmer) Apply(tokens []domain.Token) []domain.Token{
	for i := range tokens {
		if isCyrillic(tokens[i].Value) {
			tokens[i].Value = s.Ru.Stem(tokens[i].Value)
		} else {
			tokens[i].Value = s.Eng.Stem(tokens[i].Value)
		}
	}

	return tokens
}

func isCyrillic(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.Is(unicode.Cyrillic, r) {
			return false
		}
	}
	return true
}
