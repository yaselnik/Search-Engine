package loader

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

// Loader reads files from a specified root directory and saves them to the storage.
type Loader struct {
	rootPath   string
	extensions []string
	storage    domain.DocumentStorage
	logger     *slog.Logger
}

func NewLoader(
	rootPath string,
	extensions []string,
	storage domain.DocumentStorage,
	logger *slog.Logger,
) *Loader {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &Loader{
		rootPath:   rootPath,
		extensions: extensions,
		storage:    storage,
		logger:     logger.With("component", "loader"),
	}
}

// Walks the root directory, reads allowed files, and saves them to storage.
// It returns the total number of successfully loaded documents.
// Note: The current implementation uses a local ID counter.
func (l *Loader) Load(ctx context.Context) (int, error) {
	info, err := os.Stat(l.rootPath)
	if err != nil {
		return 0, fmt.Errorf("loader: stat root path %q: %w", l.rootPath, err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("loader: path %q is not a directory", l.rootPath)
	}

	var (
		loaded    int
		idCounter domain.DocID = 1
	)

	err = filepath.WalkDir(l.rootPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("loader: walk error at %q: %w", path, walkErr)
		}
		if d.IsDir() || !l.hasAllowedExtension(path) {
			return nil
		}

		doc, err := l.readFile(path, idCounter)
		if err != nil {
			l.logger.Warn("failed to read file, skipping", "path", path, "error", err)
			return nil
		}

		if err := l.storage.Add(ctx, doc); err != nil {
			l.logger.Error("failed to save document to storage", "doc_id", idCounter, "path", path, "error", err)
			return nil
		}

		loaded++
		idCounter++
		return nil
	})

	if err != nil {
		return loaded, fmt.Errorf("loader: walk directory: %w", err)
	}

	l.logger.Info("loading completed", "loaded_count", loaded, "root_path", l.rootPath)
	return loaded, nil
}

// Checks if the file path has one of the configured extensions.
func (l *Loader) hasAllowedExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, allowed := range l.extensions {
		if ext == allowed {
			return true
		}
	}
	return false
}

// Reads the file content and metadata, constructing a domain.Document.
func (l *Loader) readFile(path string, id domain.DocID) (domain.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Document{}, fmt.Errorf("loader: read file content: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return domain.Document{}, fmt.Errorf("loader: stat file path %q: %w", path, err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return domain.Document{}, fmt.Errorf("loader: get absolute path: %w", err)
	}
	baseName := filepath.Base(path)
	title := strings.TrimSuffix(baseName, filepath.Ext(baseName))

	return domain.Document{
		ID:        id,
		Title:     title,
		Content:   string(data),
		URL:       "file://" + absPath,
		CreatedAt: info.ModTime(),
		UpdatedAt: info.ModTime(),
	}, nil
}
