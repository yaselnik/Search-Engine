package indexer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

// Indexer orchestrates the process of fetching documents from storage,
// tokenizing their content, and adding them to the index.
type Indexer struct {
	storage  domain.DocumentStorage
	index    domain.Index
	analyzer domain.Analyzer
	logger   *slog.Logger

	mu      sync.Mutex
	indexed map[domain.DocID]struct{}
}

func NewIndexer(
	storage domain.DocumentStorage,
	index domain.Index,
	analyzer domain.Analyzer,
	logger *slog.Logger,
) *Indexer {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &Indexer{
		storage:  storage,
		index:    index,
		analyzer: analyzer,
		logger:   logger.With("component", "indexer"),
		indexed:  make(map[domain.DocID]struct{}),
	}
}

func (i *Indexer) IndexAll(ctx context.Context) error {
    i.mu.Lock()
    i.indexed = make(map[domain.DocID]struct{})
    i.mu.Unlock()

    _, err := i.indexPending(ctx, true)
	return err
}

func (i *Indexer) IndexNew(ctx context.Context) (int, error) {
    return i.indexPending(ctx, false)
}

func (i *Indexer) indexPending(ctx context.Context, full bool) (int, error) {
    docs, err := i.storage.GetAll(ctx)
    if err != nil {
        return 0, fmt.Errorf("indexer: get all documents: %w", err)
    }

    i.mu.Lock()
    defer i.mu.Unlock()

    var indexed int
    for _, doc := range docs {
        if ctx.Err() != nil {
            return indexed, ctx.Err()
        }

        if !full {
            if _, ok := i.indexed[doc.ID]; ok {
                continue
            }
        }

        if err := i.IndexDocument(ctx, doc); err != nil {
            i.logger.Error("index doc failed", "doc_id", doc.ID, "error", err)
            continue
        }

        i.indexed[doc.ID] = struct{}{}
        indexed++
    }

    return indexed, nil
}

// IndexDocument tokenizes a single document's content and adds it to the index.
func (i *Indexer) IndexDocument(ctx context.Context, doc domain.Document) error {
	tokens := i.analyzer.Analyze(doc.Content)

	if err := i.index.Add(doc.ID, tokens); err != nil {
		return fmt.Errorf("indexer: add tokens to index for doc %d: %w", doc.ID, err)
	}

	return nil
}
