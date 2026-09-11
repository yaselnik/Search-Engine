package storage

import (
	"context"
	"sync"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

// Compile-time check to ensure InMemoryStorage implements domain.DocumentStorage.
var _ domain.DocumentStorage = (*InMemoryStorage)(nil)

// InMemoryStorage is a thread-safe in-memory implementation of a domain.DocumentStorage.
// It maps documents dy their IDs.
type InMemoryStorage struct {
	mu   sync.RWMutex
	docs map[domain.DocID]domain.Document
}

func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{
		docs: make(map[domain.DocID]domain.Document),
	}
}

func (s *InMemoryStorage) Add(ctx context.Context, doc domain.Document) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.docs[doc.ID] = doc
	return nil
}

func (s *InMemoryStorage) GetDocumentByID(ctx context.Context, id domain.DocID) (domain.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	doc, ok := s.docs[id]
	if !ok {
		return domain.Document{}, domain.ErrDocumentNotFound
	}
	return doc, nil
}

func (s *InMemoryStorage) GetAll(ctx context.Context) ([]domain.Document, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	docs := make([]domain.Document, 0, len(s.docs))
	for _, doc := range s.docs {
		docs = append(docs, doc)
	}
	return docs, nil
}

func (s *InMemoryStorage) Delete(ctx context.Context, id domain.DocID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.docs, id)
	return nil
}

// Checks if a document with the given ID exists in the storage.
func (s *InMemoryStorage) Exists(ctx context.Context, id domain.DocID) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.docs[id]
	return ok, nil
}
