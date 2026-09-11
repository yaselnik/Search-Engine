package storage

import (
	"context"
	"testing"
	"time"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

func TestInMemoryStorage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewInMemoryStorage()
	doc := domain.Document{
		ID:        1,
		Title:     "Test Doc",
		Content:   "Hello World",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	t.Run("Add and Get", func(t *testing.T) {
		err := s.Add(ctx, doc)
		if err != nil {
			t.Fatalf("unexpected error on Add: %v", err)
		}

		got, err := s.GetDocumentByID(ctx, 1)
		if err != nil {
			t.Fatalf("unexpected error on Get: %v", err)
		}
		if got.ID != doc.ID || got.Title != doc.Title {
			t.Errorf("expected %v, got %v", doc, got)
		}
	})

	t.Run("Get Not Found", func(t *testing.T) {
		_, err := s.GetDocumentByID(ctx, 999)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err != domain.ErrDocumentNotFound {
			t.Errorf("expected ErrDocumentNotFound, got %v", err)
		}
	})

	t.Run("Exists", func(t *testing.T) {
		exists, err := s.Exists(ctx, 1)
		if err != nil || !exists {
			t.Errorf("expected document to exist")
		}
		exists, err = s.Exists(ctx, 999)
		if err != nil || exists {
			t.Errorf("expected document to not exist")
		}
	})

	t.Run("GetAll", func(t *testing.T) {
		docs, err := s.GetAll(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(docs) != 1 {
			t.Errorf("expected 1 document, got %d", len(docs))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		err := s.Delete(ctx, 1)
		if err != nil {
			t.Fatalf("unexpected error on Delete: %v", err)
		}
		exists, _ := s.Exists(ctx, 1)
		if exists {
			t.Error("expected document to be deleted")
		}
	})
}
