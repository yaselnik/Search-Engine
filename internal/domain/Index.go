package domain

// Describes the inverse index contract.
type Index interface {
	// Adds the postings of tokens for a given document.
	Add(docId DocID, tokens []Token) error

	// Retrieves the list of postings for a specific token.
	// Returns nil if the token is not found in the index.
	GetPostings(token Token) ([]Posting, error)

	// Deletes all occurrences of a document from the index.
	Remove(id DocID) error
}
