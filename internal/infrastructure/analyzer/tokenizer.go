package analyzer

import (
	"regexp"
	"strings"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

// \p{L} matches any Unicode letter, \p{N} matches any Unicode number.
var wordRegex = regexp.MustCompile(`[\p{L}\p{N}]+`)

// Splits the input text into tokens based on wordRegex expresion.
func RegexpTokenize(text string) []domain.Token {
	if text == "" {
		return []domain.Token{}
	}

	matches := wordRegex.FindAllStringIndex(text, -1)

	if matches == nil {
		return []domain.Token{}
	}

	result := make([]domain.Token, 0, len(matches))
	var wordPos uint32 = 0

	for _, match := range matches {
		token := text[match[0]:match[1]]
		wordPos++

		result = append(result, domain.Token{
			Value:    strings.ToLower(token), // Note: While analyzer pipeline are not implemented
			Origin:   token,
			Position: wordPos,
			Offset:   uint32(match[0]),
			Length:   uint16(match[1] - match[0]),
		})
	}

	return result
}
