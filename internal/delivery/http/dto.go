package http

import "github.com/yaselnik/Search-Engine/internal/domain"

// Provides an API for search queries.
type SearchResponse struct {
	Results []SearchResultItem `json:"results"`
	Count   int                `json:"count"`
	Query   string             `json:"query"`
}

// Simplified presentation of the result for the client.
type SearchResultItem struct {
	ID      uint64  `json:"id"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
	URL     string  `json:"url"`
}

// Error format
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// Converts domain.SearchResult into DTO.
func mapToSearchResultItem(res domain.SearchResult) SearchResultItem {
	return SearchResultItem{
		ID:      uint64(res.Document.ID),
		Title:   res.Document.Title,
		Score:   res.Score,
		Snippet: res.Snippet,
		URL:     res.Document.URL,
	}
}
