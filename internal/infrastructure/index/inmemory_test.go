package index

import (
	"sync"
	"testing"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

func TestInMemoryIndex_Add(t *testing.T) {
	idx := NewInMemoryIndex()

	tokens := []domain.Token{
		{Value: "hello", Position: 1},
		{Value: "world", Position: 2},
		{Value: "hello", Position: 3},
	}

	if err := idx.Add(1, tokens); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	postings, err := idx.GetPostings("hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(postings) != 1 {
		t.Fatalf("expected 1 posting, got %d", len(postings))
	}
	if postings[0].DocID != 1 {
		t.Errorf("expected DocID 1, got %d", postings[0].DocID)
	}
	if postings[0].Frequency != 2 {
		t.Errorf("expected frequency 2, got %d", postings[0].Frequency)
	}
	if len(postings[0].Positions) != 2 {
		t.Errorf("expected 2 positions, got %d", len(postings[0].Positions))
	}

	postings, _ = idx.GetPostings("world")
	if len(postings) != 1 || postings[0].Frequency != 1 {
		t.Errorf("unexpected postings for 'world': %+v", postings)
	}
}

func TestInMemoryIndex_GetPostings_NotFound(t *testing.T) {
	idx := NewInMemoryIndex()

	postings, err := idx.GetPostings("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if postings != nil {
		t.Errorf("expected nil for nonexistent token, got %+v", postings)
	}
}

func TestInMemoryIndex_Remove(t *testing.T) {
	idx := NewInMemoryIndex()

	idx.Add(1, []domain.Token{{Value: "hello", Position: 1}, {Value: "world", Position: 2}})
	idx.Add(2, []domain.Token{{Value: "hello", Position: 1}, {Value: "go", Position: 2}})

	if err := idx.Remove(1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	postings, _ := idx.GetPostings("hello")
	if len(postings) != 1 || postings[0].DocID != 2 {
		t.Errorf("expected only doc 2 for 'hello', got %+v", postings)
	}

	postings, _ = idx.GetPostings("world")
	if postings != nil {
		t.Errorf("expected nil for 'world' after removal, got %+v", postings)
	}
}

func TestInMemoryIndex_Remove_NonExistent(t *testing.T) {
	idx := NewInMemoryIndex()

	if err := idx.Remove(999); err != nil {
		t.Errorf("expected no error for non-existent doc, got %v", err)
	}
}

func TestInMemoryIndex_Add_Overwrite(t *testing.T) {
	idx := NewInMemoryIndex()

	idx.Add(1, []domain.Token{{Value: "hello", Position: 1}})

	idx.Add(1, []domain.Token{{Value: "world", Position: 1}})

	postings, _ := idx.GetPostings("hello")
	if postings != nil {
		t.Errorf("expected 'hello' to be removed after re-index, got %+v", postings)
	}

	postings, _ = idx.GetPostings("world")
	if len(postings) != 1 || postings[0].DocID != 1 {
		t.Errorf("expected 'world' in doc 1, got %+v", postings)
	}
}

func TestInMemoryIndex_ConcurrentAccess(t *testing.T) {
	idx := NewInMemoryIndex()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			docID := domain.DocID(id)
			tokens := []domain.Token{
				{Value: "token", Position: 1},
			}
			_ = idx.Add(docID, tokens)
		}(i)
	}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = idx.GetPostings("token")
		}()
	}

	wg.Wait()

	postings, _ := idx.GetPostings("token")
	if len(postings) != 100 {
		t.Errorf("expected 100 postings, got %d", len(postings))
	}
}

func BenchmarkInMemoryIndex_Add(b *testing.B) {
	idx := NewInMemoryIndex()
	tokens := make([]domain.Token, 100)
	for i := 0; i < 100; i++ {
		tokens[i] = domain.Token{Value: "token", Position: uint32(i)}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.Add(domain.DocID(i), tokens)
	}
}
