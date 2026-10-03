package domain

type Analyzer interface {
	Analyze(text string) []Token
}
