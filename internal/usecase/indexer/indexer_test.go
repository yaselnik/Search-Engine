package indexer

import (
	"context"
	"errors"
	"testing"

	"github.com/yaselnik/Search-Engine/internal/domain"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/analyzer"
)

type mockIndex struct {
	postings    map[string][]domain.Posting
	docStats    map[domain.DocID]domain.DocStats
	corpusStats domain.CorpusStats
}

func (m *mockIndex) Add(docID domain.DocID, titleTokens, contentTokens []domain.Token) error {
	return nil
}
func (m *mockIndex) Remove(id domain.DocID) error { return nil }
func (m *mockIndex) GetPostingsBatch(ctx context.Context, terms []string) (map[string][]domain.Posting, error) {
	res := make(map[string][]domain.Posting, len(terms))
	for _, t := range terms {
		if p, ok := m.postings[t]; ok {
			res[t] = p
		}
	}
	return res, nil
}
func (m *mockIndex) GetOffsetsBatch(ctx context.Context, terms []string, docIDs []domain.DocID) (map[domain.DocID][]uint32, error) {
	return nil, nil
}
func (m *mockIndex) SuggestTerms(ctx context.Context, term string, limit int) ([]string, error) {
	return nil, nil
}
func (m *mockIndex) GetCorpusStats() domain.CorpusStats {
	return m.corpusStats
}
func (m *mockIndex) GetDocStats(docID domain.DocID) domain.DocStats {
	return m.docStats[docID]
}

type mockStorage struct {
	docs []domain.Document
	err  error
}

func (m *mockStorage) GetAll(ctx context.Context) ([]domain.Document, error) {
	return m.docs, m.err
}
func (m *mockStorage) Add(ctx context.Context, doc domain.Document) error { return nil }
func (m *mockStorage) Count(ctx context.Context) (int64, error)           { return 0, nil }
func (m *mockStorage) GetDocumentByID(ctx context.Context, id domain.DocID) (domain.Document, error) {
	return domain.Document{}, nil
}
func (m *mockStorage) GetDocumentsByIDs(ctx context.Context, ids []domain.DocID) (map[domain.DocID]domain.Document, error) {
	return nil, nil
}
func (m *mockStorage) GetUnindexed(ctx context.Context, limit int) ([]domain.Document, error) {
	return []domain.Document{}, nil
}
func (m *mockStorage) MarkIndexed(ctx context.Context, ids []domain.DocID) error { return nil }
func (m *mockStorage) Delete(ctx context.Context, id domain.DocID) error         { return nil }
func (m *mockStorage) Exists(ctx context.Context, id domain.DocID) (bool, error) { return false, nil }

type mockAnalyzer struct {
	tokenizer analyzer.Tokenizer
}

func (m *mockAnalyzer) Analyze(text string) []domain.Token {
	return m.tokenizer(text)
}

func TestIndexer_IndexAll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	dummyTokenizer := func(text string) []domain.Token {
		return []domain.Token{{Value: "test", Origin: "test"}}
	}
	analyzer := mockAnalyzer{tokenizer: dummyTokenizer}

	t.Run("successful indexing", func(t *testing.T) {
		store := &mockStorage{
			docs: []domain.Document{
				{ID: 1, Content: "hello"},
				{ID: 2, Content: "world"},
			},
		}
		idx := &mockIndex{}

		ix := NewIndexer(store, idx, &analyzer, nil)

		err := ix.IndexAll(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("storage error", func(t *testing.T) {
		store := &mockStorage{err: errors.New("db error")}
		ix := NewIndexer(store, &mockIndex{}, &analyzer, nil)

		err := ix.IndexAll(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		store := &mockStorage{docs: []domain.Document{{ID: 1}}}
		ix := NewIndexer(store, &mockIndex{}, &analyzer, nil)

		err := ix.IndexAll(ctx)
		if err == nil {
			t.Fatal("expected context cancellation error, got nil")
		}
	})
}
