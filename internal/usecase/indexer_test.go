package indexer

import (
	"context"
	"errors"
	"testing"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

type mockStorage struct {
	docs []domain.Document
	err  error
}

func (m *mockStorage) GetAll(ctx context.Context) ([]domain.Document, error) {
	return m.docs, m.err
}
func (m *mockStorage) Add(ctx context.Context, doc domain.Document) error { return nil }
func (m *mockStorage) GetDocumentByID(ctx context.Context, id domain.DocID) (domain.Document, error) { return domain.Document{}, nil }
func (m *mockStorage) Delete(ctx context.Context, id domain.DocID) error { return nil }
func (m *mockStorage) Exists(ctx context.Context, id domain.DocID) (bool, error) { return false, nil }

type mockIndex struct {
	addCalled bool
	err       error
}

func (m *mockIndex) Add(docId domain.DocID, tokens []domain.Token) error {
	m.addCalled = true
	return m.err
}
func (m *mockIndex) GetPostings(token domain.Token) ([]domain.Posting, error) { return nil, nil }
func (m *mockIndex) Remove(id domain.DocID) error { return nil }
func TestIndexer_IndexAll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dummyTokenizer := func(text string) []domain.Token {
		return []domain.Token{{Value: "test", Origin: "test"}}
	}

	t.Run("successful indexing", func(t *testing.T) {
		store := &mockStorage{
			docs: []domain.Document{
				{ID: 1, Content: "hello"},
				{ID: 2, Content: "world"},
			},
		}
		idx := &mockIndex{}
		ix := NewIndexer(store, idx, dummyTokenizer, nil)

		err := ix.IndexAll(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !idx.addCalled {
			t.Error("expected index.Add to be called")
		}
	})

	t.Run("storage error", func(t *testing.T) {
		store := &mockStorage{err: errors.New("db error")}
		ix := NewIndexer(store, &mockIndex{}, dummyTokenizer, nil)

		err := ix.IndexAll(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		store := &mockStorage{docs: []domain.Document{{ID: 1}}}
		ix := NewIndexer(store, &mockIndex{}, dummyTokenizer, nil)

		err := ix.IndexAll(ctx)
		if err == nil {
			t.Fatal("expected context cancellation error, got nil")
		}
	})
}
