package index

import (
	"slices"
	"sync"

	"github.com/yaselnik/Search-Engine/internal/domain"
)
// Compile-time check to ensure InMemoryIndex implements domain.Index.
var _ domain.Index = (*InMemoryIndex)(nil)

// InMemoryIndex is a thread-safe in-memory implementation of an inverted index.
// It maps tokens to their occurrences (postings) across documents.
//
// Optimization: It maintains a reverse mapping (docTokens) to ensure that
// document removal or update operates in O(K) time, where K is the number of
// unique tokens in the document, rather than scanning the entire index.
 type InMemoryIndex struct {
	mu        sync.RWMutex
	index     map[string][]domain.Posting
	docTokens map[domain.DocID]map[string]struct{}
}

func NewInMemoryIndex() *InMemoryIndex {
	return &InMemoryIndex{
		index:     make(map[string][]domain.Posting),
		docTokens: make(map[domain.DocID]map[string]struct{}),
	}
}

// Adds the postings for a given document.
// If the document already exists, its old postings are removed before adding the new ones.
 func (r *InMemoryIndex) Add(docID domain.DocID, tokens []domain.Token) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.removeUnsafe(docID)

	tokenStats := make(map[string]*domain.Posting)
	r.docTokens[docID] = make(map[string]struct{})

	for _, token := range tokens {
		if _, exists := tokenStats[token.Value]; !exists {
			tokenStats[token.Value] = &domain.Posting{
				DocID:     docID,
				Frequency: 0,
				Positions: make([]uint32, 0, 1),
			}
		}

		posting := tokenStats[token.Value]
		posting.Frequency++
		posting.Positions = append(posting.Positions, token.Position)

		r.docTokens[docID][token.Value] = struct{}{}
	}

	for token, posting := range tokenStats {
		r.index[token] = append(r.index[token], *posting)
	}

	return nil
}


// Retrieves the list of postings for a specific token.
// Returns nil if the token is not found in the index.
func (r *InMemoryIndex) GetPostings(token domain.Token) ([]domain.Posting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	post, exist := r.index[token.Value]
	if !exist {
		return nil, nil
	}

	return post, nil
}

// Deletes all occurrences of a document from the index.
// It is a no-op if the document does not exist.
func (r *InMemoryIndex) Remove(id domain.DocID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.removeUnsafe(id)
}

// Performs the actual removal logic.
// It MUST be called only when the write lock (mu) is already held.
func (r *InMemoryIndex) removeUnsafe(id domain.DocID) error {
	tokens, exists := r.docTokens[id]
	if !exists {
		return nil
	}

	for token := range tokens {
		postings := r.index[token]
		for i, posting := range postings {
			if posting.DocID == id {
				r.index[token] = slices.Delete(postings, i, i + 1)
				break
			}
		}

		if len(r.index[token]) == 0 {
			delete(r.index, token)
		}
	}

	delete(r.docTokens, id)

	return nil
}
