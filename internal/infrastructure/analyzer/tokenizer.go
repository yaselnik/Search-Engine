package analyzer

import (
	"regexp"
	"unicode"
	"unicode/utf8"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

// Defines the contract for functions that split text into tokens.
type Tokenizer func(text string) []domain.Token

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
			Value:    token,
			Origin:   token,
			Position: wordPos,
			Offset:   uint32(match[0]),
			Length:   uint16(match[1] - match[0]),
		})
	}

	return result
}

// UTFTokenize splits the input text into tokens using a fast single-pass UTF-8 scanner.
// It keeps internal connectors ('-', '.', '\'') when immediately surrounded by letters or digits
// (e.g., "utf-8", "1.22", "wi-fi") while stripping punctuation at token boundaries.
func UTFTokenize(text string) []domain.Token {
	if text == "" {
		return []domain.Token{}
	}

	// Estimate capacity to reduce slice reallocations.
	result := make([]domain.Token, 0, len(text)/6+1)
	var wordPos uint32

	n := len(text)
	i := 0

	for i < n {
		r, width := utf8.DecodeRuneInString(text[i:])
		if !isAlphaNum(r) {
			i += width
			continue
		}

		start := i
		i += width

		for i < n {
			r, width = utf8.DecodeRuneInString(text[i:])
			if isAlphaNum(r) {
				i += width
				continue
			}

			// Allow a single internal connector ('-', '.', '\'') if followed immediately by an alphanumeric rune.
			if isInternalConnector(r) && i+width < n {
				nextR, _ := utf8.DecodeRuneInString(text[i+width:])
				if isAlphaNum(nextR) {
					i += width
					continue
				}
			}
			break
		}

		tokenStr := text[start:i]
		wordPos++

		result = append(result, domain.Token{
			Value:    tokenStr,
			Origin:   tokenStr,
			Position: wordPos,
			Offset:   uint32(start),
			Length:   uint16(min(i-start, 0xFFFF)),
		})
	}

	return result
}

func isAlphaNum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
}

func isInternalConnector(r rune) bool {
	return r == '-' || r == '.' || r == '\'' || r == '_'
}
