package domain

import "context"

// Describes the inverse index contract.
type Index interface {
	// Adds the postings of tokens for a given document.
	Add(docID DocID, titleTokens, contentTokens []Token) error
	// Deletes all occurrences of a document from the index.
	Remove(id DocID) error

	// GetPostingsBatch retrieves the list of postings (without heavy offsets) for each term for each term in a single database query
	GetPostingsBatch(ctx context.Context, terms []string) (map[string][]Posting, error)
	// GetOffsetsBatch loads byte offsets of terms only for specific documents
	GetOffsetsBatch(ctx context.Context, terms []string, docIDs []DocID) (map[DocID][]uint32, error)
	// SuggestTerms returns similar words from the dictionary based on trigrams (for correcting typos)
	SuggestTerms(ctx context.Context, term string, limit int) ([]string, error)

	GetCorpusStats() CorpusStats
	GetDocStats(docID DocID) DocStats
}
