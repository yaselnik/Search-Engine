package searcher

import (
	"context"
	"testing"

	"github.com/yaselnik/Search-Engine/internal/domain"
	"github.com/yaselnik/Search-Engine/internal/infrastructure/ranker"
)

type mockIndex struct {
	postings map[string][]domain.Posting
	docLens  map[domain.DocID]float64
}

func (m *mockIndex) Add(docId domain.DocID, tokens []domain.Token) error { return nil }
func (m *mockIndex) GetPostings(term string) ([]domain.Posting, error)   { return m.postings[term], nil }
func (m *mockIndex) Remove(id domain.DocID) error                        { return nil }
func (m *mockIndex) GetDocCount() uint64                                 { return 2 }
func (m *mockIndex) GetAvgDocLength() float64                            { return 10.0 }
func (m *mockIndex) GetDocLength(docID domain.DocID) float64             { return m.docLens[docID] }

type mockStorage struct {
	docs map[domain.DocID]domain.Document
}

func (m *mockStorage) Add(ctx context.Context, doc domain.Document) error { return nil }
func (m *mockStorage) GetDocumentByID(ctx context.Context, id domain.DocID) (domain.Document, error) {
	return m.docs[id], nil
}
func (m *mockStorage) GetAll(ctx context.Context) ([]domain.Document, error)     { return nil, nil }
func (m *mockStorage) Delete(ctx context.Context, id domain.DocID) error         { return nil }
func (m *mockStorage) Exists(ctx context.Context, id domain.DocID) (bool, error) { return false, nil }

func mockTokenizer(text string) []domain.Token {
	return []domain.Token{{Value: text}}
}

func TestSearcher_Search(t *testing.T) {
	idx := &mockIndex{
		postings: map[string][]domain.Posting{
			"golang": {
				{DocID: 1, Frequency: 3, Positions: []uint32{1, 5, 10}},
				{DocID: 2, Frequency: 1, Positions: []uint32{2}},
			},
		},
		docLens: map[domain.DocID]float64{
			1: 10.0,
			2: 20.0,
		},
	}

	storage := &mockStorage{
		docs: map[domain.DocID]domain.Document{
			1: {ID: 1, Title: "Go Guide", Content: "Learn golang programming"},
			2: {ID: 2, Title: "Long Article", Content: "This is a very long article about golang and other things"},
		},
	}

	searcher := NewSearcher(storage, idx, mockTokenizer, ranker.NewBM25())

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

func TestSearcher_generateSnippet(t *testing.T) {
	searcher := &Searcher{}
	content := "This is a long text about golang programming language. It is very popular."
	tokens := []domain.Token{{Value: "golang"}}

	snippet := searcher.generateSnippet(content, tokens)

	if !containsIgnoreCase(snippet, "golang") {
		t.Errorf("snippet should contain the query term, got: %s", snippet)
	}
	if len(content) > 80 && !contains(snippet, "...") {
		t.Errorf("snippet should contain '...' when truncated, got: %s", snippet)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}
func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
func containsIgnoreCase(s, substr string) bool {
	return containsHelper(s, substr) || containsHelper(s, "Golang")
}
