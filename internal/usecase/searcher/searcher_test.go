package searcher

import (
	"context"
	"testing"

	"github.com/yaselnik/Search-Engine/internal/domain"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/analyzer"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/ranker"
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
	docs map[domain.DocID]domain.Document
}

func (m *mockStorage) Add(ctx context.Context, doc domain.Document) error { return nil }
func (m *mockStorage) Count(ctx context.Context) (int64, error)           { return 0, nil }
func (m *mockStorage) GetDocumentsByIDs(ctx context.Context, ids []domain.DocID) (map[domain.DocID]domain.Document, error) {
	res := make(map[domain.DocID]domain.Document, len(ids))
	for _, id := range ids {
		if doc, ok := m.docs[id]; ok {
			res[id] = doc
		}
	}
	return res, nil
}
func (m *mockStorage) GetUnindexed(ctx context.Context, limit int) ([]domain.Document, error) {
	return []domain.Document{}, nil
}
func (m *mockStorage) MarkIndexed(ctx context.Context, ids []domain.DocID) error { return nil }
func (m *mockStorage) GetAll(ctx context.Context) ([]domain.Document, error)     { return nil, nil }
func (m *mockStorage) Delete(ctx context.Context, id domain.DocID) error         { return nil }
func (m *mockStorage) Exists(ctx context.Context, id domain.DocID) (bool, error) { return false, nil }

type mockAnalyzer struct {
	tokenizer analyzer.Tokenizer
}

func (m *mockAnalyzer) Analyze(text string) []domain.Token {
	return m.tokenizer(text)
}

func mockTokenizer(text string) []domain.Token {
	return []domain.Token{{Value: text}}
}

func TestSearcher_Search(t *testing.T) {
	idx := &mockIndex{
		postings: map[string][]domain.Posting{
			"golang": {
				{DocID: 1, ContentFrequency: 3, ContentPositions: []uint32{1, 5, 10}},
				{DocID: 2, ContentFrequency: 1, ContentPositions: []uint32{2}},
			},
		},
		docStats: map[domain.DocID]domain.DocStats{
			1: {ContentLength: 10.0},
			2: {ContentLength: 20.0},
		},
		corpusStats: domain.CorpusStats{
			TotalDocs:        2,
			AvgContentLength: 15.0,
		},
	}

	storage := &mockStorage{
		docs: map[domain.DocID]domain.Document{
			1: {ID: 1, Title: "Go Guide", Content: "Learn golang programming"},
			2: {ID: 2, Title: "Long Article", Content: "This is a very long article about golang and other things"},
		},
	}

	analyzer := mockAnalyzer{tokenizer: mockTokenizer}

	searcher := NewSearcher(storage, idx, &analyzer, ranker.NewBM25())

	t.Run("successful search with ranking", func(t *testing.T) {
		results, err := searcher.Search(context.Background(), "golang", 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}

		if results[0].Document.ID != 1 {
			t.Errorf("expected Doc 1 to be first, got Doc %d", results[0].Document.ID)
		}
		if results[0].Score <= results[1].Score {
			t.Errorf("expected Doc 1 score (%f) to be > Doc 2 score (%f)", results[0].Score, results[1].Score)
		}
	})

	t.Run("limit parameter is respected", func(t *testing.T) {
		results, err := searcher.Search(context.Background(), "golang", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result due to limit, got %d", len(results))
		}
	})

	t.Run("empty query returns empty results", func(t *testing.T) {
		results, err := searcher.Search(context.Background(), "", 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results for empty query, got %d", len(results))
		}
	})
}
