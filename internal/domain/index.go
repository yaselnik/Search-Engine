package domain

// Describes the inverse index contract.
type Index interface {
	// Adds the postings of tokens for a given document.
	Add(docId DocID, tokens []Token) error

	// Retrieves the list of postings for a normalized term.
	// Returns nil if the value is not found in the index.
	GetPostings(term string) ([]Posting, error)

	// Deletes all occurrences of a document from the index.
	Remove(id DocID) error

	GetDocCount() uint64
	GetAvgDocLength() float64

	// Returns document length in tokens
	GetDocLength(docID DocID) float64
}
