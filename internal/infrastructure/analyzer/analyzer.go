package analyzer

import "github.com/yaselnik/Search-Engine/internal/domain"

// Represents a text analyzer that processes input text into tokens.
// It combines a tokenizer with a chain of filters to produce normalized tokens.
type Analyzer struct {
	tokenizer Tokenizer
	filters   []Filter
}

func NewAnalyzer(tokenizer Tokenizer, filters ...Filter) *Analyzer {
	return &Analyzer{
		tokenizer: tokenizer,
		filters:   filters,
	}
}

// Tokenizes the text and applies filters one by one to the resulting sequence of tokens
func (a *Analyzer) Analyze(text string) []domain.Token {
	if text == "" {
		return nil
	}

	tokens := a.tokenizer(text)
	if len(tokens) == 0 {
		return nil
	}

	for _, filter := range a.filters {
		tokens = filter.Apply(tokens)
		if len(tokens) == 0 {
			break
		}
	}

	return tokens
}
