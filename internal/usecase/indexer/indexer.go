package indexer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/yaselnik/Search-Engine/internal/domain"
)

const defaultBatchSize = 500

// Indexer orchestrates fetching unindexed documents from storage,
// tokenizing their title and content, and updating the inverted index.
type Indexer struct {
	storage   domain.DocumentStorage
	index     domain.Index
	analyzer  domain.Analyzer
	logger    *slog.Logger
	batchSize int

	mu sync.Mutex
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
		storage:   storage,
		index:     index,
		analyzer:  analyzer,
		logger:    logger.With("component", "indexer"),
		batchSize: defaultBatchSize,
	}
}

// IndexAll re-indexes every document currently stored in DocumentStorage.
func (i *Indexer) IndexAll(ctx context.Context) error {
	docs, err := i.storage.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("indexer: get all documents: %w", err)
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	indexedIDs := make([]domain.DocID, 0, len(docs))
	for _, doc := range docs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := i.IndexDocument(ctx, doc); err != nil {
			i.logger.ErrorContext(ctx, "index doc failed", "doc_id", doc.ID, "error", err)
			continue
		}
		indexedIDs = append(indexedIDs, doc.ID)
	}

	if err := i.storage.MarkIndexed(ctx, indexedIDs); err != nil {
		return fmt.Errorf("indexer: mark indexed: %w", err)
	}
	return nil
}

// IndexNew processes all unindexed or recently updated documents in batches.
func (i *Indexer) IndexNew(ctx context.Context) (int, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	var totalIndexed int
	for {
		if err := ctx.Err(); err != nil {
			return totalIndexed, err
		}

		docs, err := i.storage.GetUnindexed(ctx, i.batchSize)
		if err != nil {
			return totalIndexed, fmt.Errorf("indexer: get unindexed documents: %w", err)
		}
		if len(docs) == 0 {
			break
		}

		indexedIDs := make([]domain.DocID, 0, len(docs))
		for _, doc := range docs {
			if err := ctx.Err(); err != nil {
				break
			}
			if err := i.IndexDocument(ctx, doc); err != nil {
				i.logger.ErrorContext(ctx, "index doc failed", "doc_id", doc.ID, "error", err)
				continue
			}
			indexedIDs = append(indexedIDs, doc.ID)
			totalIndexed++
		}

		if err := i.storage.MarkIndexed(ctx, indexedIDs); err != nil {
			return totalIndexed, fmt.Errorf("indexer: mark batch indexed: %w", err)
		}

		if len(docs) < i.batchSize {
			break
		}
	}

	return totalIndexed, nil
}

// IndexDocument tokenizes a single document's title and content separately
// and adds them to the inverted index, preserving exact byte offsets for content snippets.
func (i *Indexer) IndexDocument(_ context.Context, doc domain.Document) error {
	var titleTokens []domain.Token
	if doc.Title != "" && !strings.EqualFold(doc.Title, doc.URL) {
		titleTokens = i.analyzer.Analyze(doc.Title)
	}

	contentTokens := i.analyzer.Analyze(doc.Content)

	if err := i.index.Add(doc.ID, titleTokens, contentTokens); err != nil {
		return fmt.Errorf("indexer: add tokens to index for doc %d: %w", doc.ID, err)
	}

	return nil
}
