package loader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yaselnik/Search-Engine/internal/infrastructure/storage"
)

func TestLoader_Load(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("successful load of allowed files", func(t *testing.T) {
		dir := t.TempDir()

		os.WriteFile(filepath.Join(dir, "doc1.txt"), []byte("content 1"), 0644)
		os.WriteFile(filepath.Join(dir, "doc2.md"), []byte("content 2"), 0644)
		os.WriteFile(filepath.Join(dir, "image.jpg"), []byte("fake image"), 0644)
		os.Mkdir(filepath.Join(dir, "subdir"), 0755)
		os.WriteFile(filepath.Join(dir, "subdir", "doc3.txt"), []byte("content 3"), 0644)

		memStorage := storage.NewInMemoryStorage()
		l := NewLoader(dir, []string{".txt", ".md"}, memStorage, nil)

		loaded, err := l.Load(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if loaded != 3 {
			t.Errorf("expected 3 loaded documents, got %d", loaded)
		}

		docs, _ := memStorage.GetAll(ctx)
		if len(docs) != 3 {
			t.Errorf("expected 3 documents in storage, got %d", len(docs))
		}
	})

	t.Run("invalid root path", func(t *testing.T) {
		l := NewLoader("/non/existent/path", []string{".txt"}, storage.NewInMemoryStorage(), nil)
		_, err := l.Load(ctx)
		if err == nil {
			t.Fatal("expected error for non-existent path")
		}
	})
}

func TestLoader_hasAllowedExtension(t *testing.T) {
	t.Parallel()
	l := NewLoader("", []string{".txt", ".md"}, nil, nil)

	tests := []struct {
		path     string
		expected bool
	}{
		{"/path/to/file.txt", true},
		{"/path/to/file.TXT", true},
		{"/path/to/file.md", true},
		{"/path/to/file.jpg", false},
		{"/path/to/file", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := l.hasAllowedExtension(tt.path); got != tt.expected {
				t.Errorf("hasAllowedExtension(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}
