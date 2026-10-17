package inmemoryStorage

import (
	"context"
	"sync"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

// Compile-time check to ensure InMemoryStorage implements domain.DocumentStorage.
var _ domain.DocumentStorage = (*InMemoryStorage)(nil)

// InMemoryStorage is a thread-safe in-memory implementation of a domain.DocumentStorage.
type InMemoryStorage struct {
	mu   sync.RWMutex
	docs map[domain.DocID]domain.Document
	unindexed map[domain.DocID]struct{}
}

// NewInMemoryStorage creates an empty InMemoryStorage.
func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{
		docs:      make(map[domain.DocID]domain.Document),
		unindexed: make(map[domain.DocID]struct{}),
	}
}

// Add stores or updates a document and marks it as needing indexing.
func (s *InMemoryStorage) Add(ctx context.Context, doc domain.Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.docs[doc.ID] = doc
	s.unindexed[doc.ID] = struct{}{}
	return nil
}

func (s *InMemoryStorage) Count(_ context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.docs)), nil
}

// GetDocumentByID retrieves a document by ID or returns domain.ErrDocumentNotFound.
func (s *InMemoryStorage) GetDocumentByID(ctx context.Context, id domain.DocID) (domain.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	doc, ok := s.docs[id]
	if !ok {
		return domain.Document{}, domain.ErrDocumentNotFound
	}
	return doc, nil
}

// GetAll returns a slice of all documents in storage.
func (s *InMemoryStorage) GetAll(ctx context.Context) ([]domain.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	docs := make([]domain.Document, 0, len(s.docs))
	for _, doc := range s.docs {
		docs = append(docs, doc)
	}
	return docs, nil
}

// GetUnindexed returns up to limit documents that have not yet been marked as indexed.
func (s *InMemoryStorage) GetUnindexed(ctx context.Context, limit int) ([]domain.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.unindexed) {
		limit = len(s.unindexed)
	}
	docs := make([]domain.Document, 0, limit)
	for id := range s.unindexed {
		if len(docs) >= limit {
			break
		}
		if doc, ok := s.docs[id]; ok {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

// MarkIndexed removes the given document IDs from the unindexed set.
func (s *InMemoryStorage) MarkIndexed(ctx context.Context, ids []domain.DocID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range ids {
		delete(s.unindexed, id)
	}
	return nil
}

// Delete removes a document and its unindexed marker from storage.
func (s *InMemoryStorage) Delete(ctx context.Context, id domain.DocID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.docs, id)
	delete(s.unindexed, id)
	return nil
}

// Exists checks if a document with the given ID exists in storage.
func (s *InMemoryStorage) Exists(ctx context.Context, id domain.DocID) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.docs[id]
	return ok, nil
}
