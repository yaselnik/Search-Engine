package domain

// Represents a single matched document returned by the search engine.
// It includes the original document, its relevance score, and a contextual snippet.
type SearchResult struct {
	Document Document
	Score    float64
	Snippet  string
}
