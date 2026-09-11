package indexer

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

// Indexer orchestrates the process of fetching documents from storage,
// tokenizing their content, and adding them to the index.
type Indexer struct {
	storage   domain.DocumentStorage
	index     domain.Index
	tokenizer domain.Tokenizer
	logger    *slog.Logger
}

func NewIndexer(
	storage domain.DocumentStorage,
	index domain.Index,
	tokenizer domain.Tokenizer,
	logger *slog.Logger,
) *Indexer {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &Indexer{
		storage:   storage,
		index:     index,
		tokenizer: tokenizer,
		logger:    logger.With("component", "indexer"),
	}
}

// Retrieves all documents from the storage and indexes them sequentially.
// It respects context cancellation and continues processing even if a single document
func (i *Indexer) IndexAll(ctx context.Context) error {
	docs, err := i.storage.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("indexer: get all documents from storage: %w", err)
	}

	i.logger.Info("starting indexing process", "total_documents", len(docs))

	var indexed int
	for _, doc := range docs {
		if ctx.Err() != nil {
			i.logger.Warn("indexing cancelled via context", "indexed_so_far", indexed)
			return fmt.Errorf("indexer: context cancelled: %w", ctx.Err())
		}

		if err := i.IndexDocument(ctx, doc); err != nil {
			i.logger.Error("failed to index document", "doc_id", doc.ID, "title", doc.Title, "error", err)
			continue
		}
		indexed++
	}

	i.logger.Info("indexing process completed", "indexed", indexed, "total", len(docs))
	return nil
}

// IndexDocument tokenizes a single document's content and adds it to the index.
func (i *Indexer) IndexDocument(ctx context.Context, doc domain.Document) error {
	tokens := i.tokenizer(doc.Content)

	if err := i.index.Add(doc.ID, tokens); err != nil {
		return fmt.Errorf("indexer: add tokens to index for doc %d: %w", doc.ID, err)
	}

	return nil
}
